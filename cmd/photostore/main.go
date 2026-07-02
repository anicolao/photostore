package main

import (
	"flag"
	"fmt"
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
			scanID, err := st.ScanInventory(*inventory, *invType, exts, *resolverRoot, splitCSV(*strip), true)
			if err != nil {
				return err
			}
			fmt.Println(scanID)
			return nil
		default:
			return usage()
		}
	case "scan":
		fs := flag.NewFlagSet("scan", flag.ExitOnError)
		storePath := fs.String("store", "./photostore-data", "store path")
		_ = fs.Int("workers", 0, "accepted for compatibility; acquisition is currently serialized")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		st, err := photostore.Open(*storePath)
		if err != nil {
			return err
		}
		defer st.Close()
		scanID, err := st.ScanSources()
		if err != nil {
			return err
		}
		fmt.Println(scanID)
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
		report, err := st.Report(*scanID)
		if err != nil {
			return err
		}
		fmt.Printf("scan: %s\nsource files acquired: %d\nhistorical entries already seen: %d\n", report.ScanID, report.SourceFilesAcquired, report.HistoricalEntriesAlreadySeen)
		return nil
	default:
		return usage()
	}
}

func usage() error {
	return fmt.Errorf("usage: photostore init|source add|inventory acquire|inventory scan|scan|report")
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
