package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/anicolao/photostore/internal/photostore"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "photostore:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "init":
		fs := flag.NewFlagSet("init", flag.ExitOnError)
		storePath := fs.String("store", "./photostore-data", "store path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		st, err := photostore.Init(*storePath)
		if err != nil {
			return err
		}
		defer st.Close()
		fmt.Println(*storePath)
		return nil
	case "source":
		if len(args) < 2 || args[1] != "add" {
			return usage()
		}
		fs := flag.NewFlagSet("source add", flag.ExitOnError)
		storePath := fs.String("store", "./photostore-data", "store path")
		path := fs.String("path", "", "source root path")
		label := fs.String("label", "", "source label")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if *path == "" {
			return fmt.Errorf("--path is required")
		}
		st, err := photostore.Open(*storePath)
		if err != nil {
			return err
		}
		defer st.Close()
		id, err := st.AddSourceRoot(*path, *label)
		if err != nil {
			return err
		}
		fmt.Println(id)
		return nil
	case "inventory":
		if len(args) < 2 {
			return usage()
		}
		switch args[1] {
		case "acquire":
			fs := flag.NewFlagSet("inventory acquire", flag.ExitOnError)
			storePath := fs.String("store", "./photostore-data", "store path")
			path := fs.String("path", "", "inventory path")
			label := fs.String("label", "", "inventory label")
			group := fs.String("group", "", "inventory group")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if *path == "" {
				return fmt.Errorf("--path is required")
			}
			st, err := photostore.Open(*storePath)
			if err != nil {
				return err
			}
			defer st.Close()
			id, err := st.AcquireInventory(*path, *label, *group)
			if err != nil {
				return err
			}
			fmt.Println(id)
			return nil
		case "scan":
			fs := flag.NewFlagSet("inventory scan", flag.ExitOnError)
			storePath := fs.String("store", "./photostore-data", "store path")
			inventory := fs.String("inventory", "", "inventory id")
			invType := fs.String("type", "toc", "inventory type")
			resolverRoot := fs.String("resolver-root", "", "resolver root")
			strip := fs.String("strip-prefixes", "./", "comma-separated prefixes to strip")
			verbose := fs.Bool("verbose", false, "print progress and final report")
			var exts repeated
			fs.Var(&exts, "ext", "extension to ingest")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if *inventory == "" {
				return fmt.Errorf("--inventory is required")
			}
			st, err := photostore.Open(*storePath)
			if err != nil {
				return err
			}
			defer st.Close()
			scanID, err := st.ScanInventoryWithProgress(*inventory, *invType, exts, *resolverRoot, splitCSV(*strip), true, progress(*verbose))
			if err != nil {
				return err
			}
			fmt.Println(scanID)
			if *verbose {
				return printReport(st, scanID)
			}
			return nil
		default:
			return usage()
		}
	case "scan":
		fs := flag.NewFlagSet("scan", flag.ExitOnError)
		storePath := fs.String("store", "./photostore-data", "store path")
		workers := fs.Int("workers", 0, "parallel acquisition workers; defaults to a bounded CPU-based value")
		verbose := fs.Bool("verbose", false, "print progress and final report")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		st, err := photostore.Open(*storePath)
		if err != nil {
			return err
		}
		defer st.Close()
		scanID, err := st.ScanSourcesWithOptions(progress(*verbose), photostore.ScanOptions{Workers: *workers})
		if err != nil {
			return err
		}
		fmt.Println(scanID)
		if *verbose {
			return printReport(st, scanID)
		}
		return nil
	case "report":
		fs := flag.NewFlagSet("report", flag.ExitOnError)
		storePath := fs.String("store", "./photostore-data", "store path")
		scanID := fs.String("scan-id", "", "scan id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *scanID == "" {
			return fmt.Errorf("--scan-id is required")
		}
		st, err := photostore.Open(*storePath)
		if err != nil {
			return err
		}
		defer st.Close()
		return printReport(st, *scanID)
	case "metadata":
		if len(args) < 2 || args[1] != "refresh-missing" {
			return usage()
		}
		fs := flag.NewFlagSet("metadata refresh-missing", flag.ExitOnError)
		storePath := fs.String("store", "./photostore-data", "store path")
		verbose := fs.Bool("verbose", false, "print progress and final summary")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		st, err := photostore.Open(*storePath)
		if err != nil {
			return err
		}
		defer st.Close()
		summary, err := st.RefreshMissingMetadata(progress(*verbose))
		if err != nil {
			return err
		}
		fmt.Println(summary.RequestID)
		if *verbose {
			fmt.Printf("attempted: %d\n", summary.Attempted)
			fmt.Printf("extracted: %d\n", summary.Extracted)
			fmt.Printf("failed: %d\n", summary.Failed)
			fmt.Printf("skipped: %d\n", summary.Skipped)
			fmt.Printf("issues: %d\n", summary.Issues)
		}
		return nil
	case "deduplicate":
		fs := flag.NewFlagSet("deduplicate", flag.ExitOnError)
		storePath := fs.String("store", "./photostore-data", "store path")
		verbose := fs.Bool("verbose", false, "print progress and final summary")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		st, err := photostore.Open(*storePath)
		if err != nil {
			return err
		}
		defer st.Close()
		summary, err := st.VerifyAndDeduplicate(progress(*verbose))
		if err != nil {
			return err
		}
		fmt.Println(summary.RequestID)
		if *verbose {
			fmt.Printf("candidates: %d\n", summary.Candidates)
			fmt.Printf("deduplicated: %d\n", summary.Deduplicated)
			fmt.Printf("bytes released: %d\n", summary.BytesReleased)
			fmt.Printf("verification errors: %d\n", summary.VerificationErrors)
			fmt.Printf("relink errors: %d\n", summary.RelinkErrors)
		}
		return nil
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		storePath := fs.String("store", "./photostore-data", "store path")
		addr := fs.String("addr", "127.0.0.1:8080", "listen address")
		apiOnly := fs.Bool("api-only", false, "serve only API routes")
		allowPublicBind := fs.Bool("allow-public-bind", false, "allow binding to non-loopback addresses")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if !*allowPublicBind && !isLoopbackAddr(*addr) {
			return fmt.Errorf("refusing to bind %s; use --allow-public-bind to serve beyond loopback", *addr)
		}
		st, err := photostore.Open(*storePath)
		if err != nil {
			return err
		}
		defer st.Close()
		srv := photostore.NewServer(st, photostore.ServerOptions{APIOnly: *apiOnly})
		fmt.Fprintf(os.Stderr, "photostore serving http://%s\n", *addr)
		return http.ListenAndServe(*addr, srv)
	default:
		return usage()
	}
}

func usage() error {
	return fmt.Errorf("usage: photostore init|source add|inventory acquire|inventory scan|scan|report|metadata refresh-missing|deduplicate|serve")
}

func progress(verbose bool) photostore.ProgressFunc {
	if !verbose {
		return nil
	}
	return func(message string) {
		fmt.Fprintln(os.Stderr, message)
	}
}

func printReport(st *photostore.Store, scanID string) error {
	report, err := st.Report(scanID)
	if err != nil {
		return err
	}
	fmt.Printf("scan: %s\n", report.ScanID)
	fmt.Printf("source files acquired: %d\n", report.SourceFilesAcquired)
	fmt.Printf("duplicate acquisitions: %d\n", report.DuplicateAcquisitions)
	fmt.Printf("duplicate garbage bytes: %d\n", report.DuplicateGarbageBytes)
	fmt.Printf("historical entries already seen: %d\n", report.HistoricalEntriesAlreadySeen)
	return nil
}

type repeated []string

func (r *repeated) String() string {
	return strings.Join(*r, ",")
}

func (r *repeated) Set(v string) error {
	*r = append(*r, v)
	return nil
}

func splitCSV(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
