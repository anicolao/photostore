package photostore

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type DeduplicateSummary struct {
	RequestID          string `json:"request_id"`
	Candidates         int    `json:"candidates"`
	Deduplicated       int    `json:"deduplicated"`
	BytesReleased      int64  `json:"bytes_released"`
	VerificationErrors int    `json:"verification_errors"`
	RelinkErrors       int    `json:"relink_errors"`
}

const (
	dedupStrategyName    = "hard_link"
	dedupStrategyVersion = 3
)

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

type dedupCandidateResult struct {
	Candidate duplicateCandidate
	Result    fileVerification
	Method    string
	Err       error
}

func (s *Store) VerifyAndDeduplicate(progress ProgressFunc) (DeduplicateSummary, error) {
	requestID := newID("dedup_req")
	requestEventID, err := s.appendEventReturnID("DuplicateDeduplicationRequested", nil, nil, map[string]any{
		"request_id":      requestID,
		"requested_at_ms": nowMS(),
		"selector": map[string]any{
			"type": "retained_source_objects",
		},
		"verification": map[string]any{
			"hash_algorithm":                 "sha256",
			"requires_byte_comparison":       true,
			"requires_canonical_rehash":      true,
			"requires_duplicate_rehash":      true,
			"delete_duplicate_before_relink": true,
		},
		"strategy": dedupStrategyPayload(),
	})
	if err != nil {
		return DeduplicateSummary{}, err
	}
	candidates, err := s.duplicateCandidates()
	if err != nil {
		return DeduplicateSummary{}, err
	}
	summary := DeduplicateSummary{RequestID: requestID, Candidates: len(candidates)}
	jobs := make(chan duplicateCandidate)
	results := make(chan dedupCandidateResult, len(candidates))
	var wg sync.WaitGroup
	workers := dedupWorkers()
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for candidate := range jobs {
				result, method, err := s.verifyAndDeduplicateCandidate(candidate)
				results <- dedupCandidateResult{
					Candidate: candidate,
					Result:    result,
					Method:    method,
					Err:       err,
				}
			}
		}()
	}
	go func() {
		for _, candidate := range candidates {
			jobs <- candidate
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	var appendErr error
	processed := 0
	for candidateResult := range results {
		processed++
		candidate := candidateResult.Candidate
		if candidateResult.Err != nil {
			var relinkErr dedupRelinkError
			if errors.As(candidateResult.Err, &relinkErr) {
				summary.RelinkErrors++
				progressCountf(progress, processed, summary.Candidates, "duplicate relink failed for %s: %v", candidate.StoredObjectID, candidateResult.Err)
			} else {
				summary.VerificationErrors++
				progressCountf(progress, processed, summary.Candidates, "duplicate verification failed for %s: %v", candidate.StoredObjectID, candidateResult.Err)
			}
			continue
		}
		if err := s.appendEvent("DuplicateSourceObjectDeduplicated", &requestEventID, nil, map[string]any{
			"request_id":           requestID,
			"source_occurrence_id": candidate.SourceOccurrenceID,
			"stored_object_id":     candidate.StoredObjectID,
			"content_ref":          candidate.ContentRef,
			"deduplicated_at_ms":   nowMS(),
			"strategy":             dedupStrategyPayload(),
			"verification": map[string]any{
				"hash_algorithm":  "sha256",
				"canonical_hash":  candidateResult.Result.CanonicalHash,
				"duplicate_hash":  candidateResult.Result.DuplicateHash,
				"byte_comparison": candidateResult.Result.BytesEqual,
				"bytes_compared":  candidateResult.Result.Size,
			},
			"storage": map[string]any{
				"duplicate_deleted": true,
				"replacement": map[string]any{
					"method": candidateResult.Method,
				},
			},
		}); err != nil {
			if appendErr == nil {
				appendErr = err
			}
			continue
		}
		summary.Deduplicated++
		summary.BytesReleased += candidateResult.Result.Size
		progressCountf(progress, processed, summary.Candidates, "deduplicated %s (%d bytes via %s)", candidate.StoredObjectID, candidateResult.Result.Size, candidateResult.Method)
	}
	if appendErr != nil {
		return summary, appendErr
	}
	progressf(progress, "deduplicated: %d/%d, bytes released: %d, verification errors: %d, relink errors: %d", summary.Deduplicated, summary.Candidates, summary.BytesReleased, summary.VerificationErrors, summary.RelinkErrors)
	return summary, nil
}

func dedupWorkers() int {
	return workersFromEnv("PHOTOSTORE_DEDUP_WORKERS")
}

func (s *Store) duplicateCandidates() ([]duplicateCandidate, error) {
	rows, err := s.DB.Query(`
		select scl.source_occurrence_id, scl.stored_object_id, scl.content_ref, st.acquired_storage_key
		from source_content_links scl
		join stored_objects st on st.stored_object_id = scl.stored_object_id
		where not exists (
				select 1 from duplicate_deduplications dd
				where dd.source_occurrence_id = scl.source_occurrence_id
					and dd.strategy_name = ?
					and dd.strategy_version = ?
			)
		order by scl.source_occurrence_id`, dedupStrategyName, dedupStrategyVersion)
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

func dedupStrategyPayload() map[string]any {
	return map[string]any{
		"name":    dedupStrategyName,
		"version": dedupStrategyVersion,
	}
}

func (s *Store) rebuildDuplicateDeduplicationProjection() error {
	if _, err := s.DB.Exec(`delete from duplicate_deduplications`); err != nil {
		return err
	}
	f, err := os.Open(filepath.Join(s.Root, "events", "events.jsonl"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024), 1024*1024*16)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return err
		}
		if ev.EventType != "DuplicateSourceObjectDeduplicated" {
			continue
		}
		strategy := mapValue(ev.Payload["strategy"])
		storage := mapValue(ev.Payload["storage"])
		replacement := mapValue(storage["replacement"])
		if _, err := s.DB.Exec(`insert or replace into duplicate_deduplications values(?,?,?,?,?,?,?,?)`, str(ev.Payload["source_occurrence_id"]), str(ev.Payload["stored_object_id"]), str(ev.Payload["content_ref"]), ev.EventID, int64Value(ev.Payload["deduplicated_at_ms"]), str(strategy["name"]), int64Value(strategy["version"]), str(replacement["method"])); err != nil {
			return err
		}
	}
	return sc.Err()
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
	if err := os.Link(canonicalPath, duplicatePath); err != nil {
		return result, "", dedupRelinkError{err: err}
	}
	if err := chmodCompleteFile(duplicatePath); err != nil {
		return result, "", dedupRelinkError{err: err}
	}
	return result, "hard_link", nil
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
