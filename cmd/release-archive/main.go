package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	output := flag.String("output", "", "zip archive path")
	file := flag.String("file", "", "file to archive")
	flag.Parse()
	if *output == "" || *file == "" {
		fail(fmt.Errorf("-output and -file are required"))
	}

	in, err := os.Open(*file)
	if err != nil {
		fail(err)
	}
	defer in.Close()

	out, err := os.Create(*output)
	if err != nil {
		fail(err)
	}
	zipper := zip.NewWriter(out)
	entry, err := zipper.Create(filepath.Base(*file))
	if err == nil {
		_, err = io.Copy(entry, in)
	}
	if closeErr := zipper.Close(); err == nil {
		err = closeErr
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
