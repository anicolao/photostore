package photostore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

type DeduplicateSummary struct {
	RequestID          string `json:"request_id"`
	Candidates         int    `json:"candidates"`
	Deduplicated       int    `json:"deduplicated"`
	BytesReleased      int64  `json:"bytes_released"`
	VerificationErrors int    `json:"verification_errors"`
	RelinkErrors       int    `json:"relink_errors"`
}

type duplicateCandidate struct {
	SourceOccurrenceID string
	StoredObjectID     string
	ContentRef         string
	AcquiredStorageKey string
	Size               int64
}

type fileVerification struct {
	Size          int64
	CanonicalHash string
	DuplicateHash string
	BytesEqual    bool
}

type dedupRelinkError struct {
	err error
}

func (e dedupRelinkError) Error() string {
	return e.err.Error()
}

func (s *Store) VerifyAndDeduplicate(progress ProgressFunc) (DeduplicateSummary, error) {
	requestID := newID("dedup_req")
	requestEventID, err := s.appendEventReturnID("DuplicateDeduplicationRequested", nil, nil, map[string]any{
		"request_id":      requestID,
		"requested_at_ms": nowMS(),
		"selector": map[string]any{
			"type": "retained_duplicate_source_objects",
		},
		"verification": map[string]any{
			"hash_algorithm":                 "sha256",
			"requires_byte_comparison":       true,
			"requires_canonical_rehash":      true,
			"requires_duplicate_rehash":      true,
			"replacement_order":              []string{"apfs_clone", "hard_link"},
			"delete_duplicate_before_relink": true,
		},
	})
	if err != nil {
		return DeduplicateSummary{}, err
	}
	candidates, err := s.duplicateCandidates()
	if err != nil {
		return DeduplicateSummary{}, err
	}
	summary := DeduplicateSummary{RequestID: requestID, Candidates: len(candidates)}
	for _, candidate := range candidates {
		progressf(progress, "verifying duplicate %s", candidate.StoredObjectID)
		result, method, err := s.verifyAndDeduplicateCandidate(candidate)
		if err != nil {
			var relinkErr dedupRelinkError
			if errors.As(err, &relinkErr) {
				summary.RelinkErrors++
				progressf(progress, "duplicate relink failed for %s: %v", candidate.StoredObjectID, err)
			} else {
				summary.VerificationErrors++
				progressf(progress, "duplicate verification failed for %s: %v", candidate.StoredObjectID, err)
			}
			continue
		}
		if err := s.appendEvent("DuplicateSourceObjectDeduplicated", &requestEventID, nil, map[string]any{
			"request_id":           requestID,
			"source_occurrence_id": candidate.SourceOccurrenceID,
			"stored_object_id":     candidate.StoredObjectID,
			"content_ref":          candidate.ContentRef,
			"deduplicated_at_ms":   nowMS(),
			"verification": map[string]any{
				"hash_algorithm":  "sha256",
				"canonical_hash":  result.CanonicalHash,
				"duplicate_hash":  result.DuplicateHash,
				"byte_comparison": result.BytesEqual,
				"bytes_compared":  result.Size,
			},
			"storage": map[string]any{
				"duplicate_deleted": true,
				"replacement": map[string]any{
					"method": method,
				},
			},
		}); err != nil {
			return summary, err
		}
		summary.Deduplicated++
		summary.BytesReleased += result.Size
		progressf(progress, "deduplicated %s (%d bytes via %s)", candidate.StoredObjectID, result.Size, method)
	}
	progressf(progress, "deduplicated: %d/%d, bytes released: %d, verification errors: %d, relink errors: %d", summary.Deduplicated, summary.Candidates, summary.BytesReleased, summary.VerificationErrors, summary.RelinkErrors)
	return summary, nil
}

func (s *Store) duplicateCandidates() ([]duplicateCandidate, error) {
	rows, err := s.DB.Query(`
		select scl.source_occurrence_id, scl.stored_object_id, scl.content_ref, st.acquired_storage_key
		from source_content_links scl
		join stored_objects st on st.stored_object_id = scl.stored_object_id
		where scl.cas_existed_at_ingest = 1
			and scl.acquired_object_retained = 1
		order by scl.source_occurrence_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []duplicateCandidate
	for rows.Next() {
		var candidate duplicateCandidate
		if err := rows.Scan(&candidate.SourceOccurrenceID, &candidate.StoredObjectID, &candidate.ContentRef, &candidate.AcquiredStorageKey); err != nil {
			return nil, err
		}
		_, _, candidate.Size = parseContentRef(candidate.ContentRef)
		out = append(out, candidate)
	}
	return out, rows.Err()
}

func (s *Store) verifyAndDeduplicateCandidate(candidate duplicateCandidate) (fileVerification, string, error) {
	canonicalPath := filepath.Join(s.Root, filepath.FromSlash(casKey(candidate.ContentRef)))
	duplicatePath := filepath.Join(s.Root, filepath.FromSlash(candidate.AcquiredStorageKey))
	result, err := verifyDuplicateFiles(canonicalPath, duplicatePath)
	if err != nil {
		return fileVerification{}, "", err
	}
	if !result.BytesEqual {
		return fileVerification{}, "", errors.New("canonical and duplicate bytes differ")
	}
	_, expectedHash, expectedSize := parseContentRef(candidate.ContentRef)
	if result.Size != expectedSize {
		return fileVerification{}, "", fmt.Errorf("verified size = %d, want %d", result.Size, expectedSize)
	}
	if result.CanonicalHash != expectedHash || result.DuplicateHash != expectedHash {
		return fileVerification{}, "", fmt.Errorf("verified hash mismatch for %s", candidate.ContentRef)
	}
	if err := os.Remove(duplicatePath); err != nil {
		return result, "", dedupRelinkError{err: err}
	}
	method, err := cloneOrHardLink(canonicalPath, duplicatePath)
	if err != nil {
		return result, "", dedupRelinkError{err: err}
	}
	return result, method, nil
}

func verifyDuplicateFiles(canonicalPath, duplicatePath string) (fileVerification, error) {
	canonical, err := os.Open(canonicalPath)
	if err != nil {
		return fileVerification{}, err
	}
	defer canonical.Close()
	duplicate, err := os.Open(duplicatePath)
	if err != nil {
		return fileVerification{}, err
	}
	defer duplicate.Close()
	if _, err := canonical.Stat(); err != nil {
		return fileVerification{}, err
	}
	duplicateInfo, err := duplicate.Stat()
	if err != nil {
		return fileVerification{}, err
	}
	canonicalHash := sha256.New()
	duplicateHash := sha256.New()
	bufA := make([]byte, 1024*1024)
	bufB := make([]byte, 1024*1024)
	for {
		nA, errA := canonical.Read(bufA)
		nB, errB := duplicate.Read(bufB)
		if nA > 0 {
			_, _ = canonicalHash.Write(bufA[:nA])
		}
		if nB > 0 {
			_, _ = duplicateHash.Write(bufB[:nB])
		}
		if nA != nB || !bytes.Equal(bufA[:nA], bufB[:nB]) {
			_, _ = io.Copy(canonicalHash, canonical)
			_, _ = io.Copy(duplicateHash, duplicate)
			return fileVerification{
				Size:          duplicateInfo.Size(),
				CanonicalHash: hex.EncodeToString(canonicalHash.Sum(nil)),
				DuplicateHash: hex.EncodeToString(duplicateHash.Sum(nil)),
				BytesEqual:    false,
			}, nil
		}
		if errA == io.EOF && errB == io.EOF {
			return fileVerification{
				Size:          duplicateInfo.Size(),
				CanonicalHash: hex.EncodeToString(canonicalHash.Sum(nil)),
				DuplicateHash: hex.EncodeToString(duplicateHash.Sum(nil)),
				BytesEqual:    true,
			}, nil
		}
		if errA != nil && errA != io.EOF {
			return fileVerification{}, errA
		}
		if errB != nil && errB != io.EOF {
			return fileVerification{}, errB
		}
	}
}

func cloneOrHardLink(src, dst string) (string, error) {
	if err := exec.Command("cp", "-c", src, dst).Run(); err == nil {
		return "apfs_clone", nil
	}
	if err := os.Link(src, dst); err == nil {
		return "hard_link", nil
	} else {
		return "", err
	}
}
