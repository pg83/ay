package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func unpackStrippedResourceArchive(archivePath, out string, stripPrefix uint32) {
	file := throw2(os.Open(archivePath))

	defer file.Close()

	root := throw2(os.OpenRoot(out))

	defer root.Close()

	reader := bufio.NewReader(file)
	magic, err := reader.Peek(4)

	if err != nil {
		throwFmt("fetch: unrecognized resource archive: %v", err)
	}

	if string(magic[:2]) == "PK" {
		archive := throw2(zip.NewReader(file, throw2(file.Stat()).Size()))

		for _, entry := range archive.File {
			extractStrippedResourceZipEntry(root, entry, stripPrefix)
		}

		return
	}

	extract := func(stream io.Reader) {
		extractStrippedResourceTar(root, stream, stripPrefix)
		throw2(io.Copy(io.Discard, stream))
	}

	switch {
	case magic[0] == 0x1f && magic[1] == 0x8b:
		stream := throw2(gzip.NewReader(reader))

		defer stream.Close()

		extract(stream)
	case string(magic[:3]) == "BZh":
		extract(bzip2.NewReader(reader))
	case string(magic) == "\xfd7zX":
		extractCompressedResourceTar("xz", reader, extract)
	case string(magic) == "\x28\xb5\x2f\xfd":
		extractCompressedResourceTar("zstd", reader, extract)
	default:
		extract(reader)
	}
}

func extractStrippedResourceZipEntry(root *os.Root, entry *zip.File, stripPrefix uint32) {
	data := throw2(entry.Open())

	defer data.Close()

	header := throw2(tar.FileInfoHeader(entry.FileInfo(), ""))

	header.Name = entry.Name

	if entry.Mode()&os.ModeSymlink != 0 {
		target := throw2(io.ReadAll(io.LimitReader(data, 65537)))

		if len(target) > 65536 {
			throwFmt("fetch: resource archive symlink target exceeds 65536 bytes")
		}

		header.Linkname = string(target)
	}

	extractStrippedResourceEntry(root, header, data, stripPrefix)
}

func extractCompressedResourceTar(program string, input io.Reader, extract func(io.Reader)) {
	command := exec.Command(program, "-dc")

	command.Stdin = input
	command.Stderr = os.Stderr

	stream := throw2(command.StdoutPipe())

	throw(command.Start())

	err := try(func() { extract(stream) })
	closeErr := stream.Close()
	waitErr := command.Wait()

	if err != nil {
		err.throw()
	}

	throw(closeErr)
	throw(waitErr)
}

func extractStrippedResourceTar(root *os.Root, stream io.Reader, stripPrefix uint32) {
	archive := tar.NewReader(stream)

	for {
		header, err := archive.Next()

		if err == io.EOF {
			return
		}

		throw(err)
		extractStrippedResourceEntry(root, header, archive, stripPrefix)
	}
}

func stripResourceArchivePath(name string, stripPrefix uint32) string {
	parts := strings.Split(filepath.ToSlash(name), "/")
	out := make([]string, 0, len(parts))

	if strings.HasPrefix(name, "/") {
		out = append(out, "/")
	}

	for _, part := range parts {
		if part != "" && part != "." {
			out = append(out, part)
		}
	}

	if uint64(stripPrefix) >= uint64(len(out)) {
		return ""
	}

	return strings.Join(out[stripPrefix:], "/")
}

func extractStrippedResourceEntry(root *os.Root, header *tar.Header, data io.Reader, stripPrefix uint32) {
	name := stripResourceArchivePath(header.Name, stripPrefix)

	if name == "" {
		return
	}

	if !filepath.IsLocal(name) {
		throwFmt("fetch: archive path %q escapes resource output", name)
	}

	if header.Typeflag == tar.TypeDir || strings.HasSuffix(header.Name, "/") {
		throw(root.MkdirAll(name, 0o755))

		return
	}

	if _, err := root.Stat(name); err == nil {
		throwFmt("fetch: archive file %q is duplicated because of strip_components=%d", name, stripPrefix)
	} else if !os.IsNotExist(err) {
		throw(err)
	}

	throw(root.MkdirAll(filepath.Dir(name), 0o755))

	switch header.Typeflag {
	case tar.TypeSymlink:
		target := stripResourceArchivePath(header.Linkname, stripPrefix)

		if !filepath.IsLocal(filepath.Join(filepath.Dir(name), target)) {
			throwFmt("fetch: archive symlink %q escapes resource output", name)
		}

		throw(root.Symlink(target, name))
	case tar.TypeLink:
		throw(root.Link(stripResourceArchivePath(header.Linkname, stripPrefix), name))
	default:
		file := throw2(root.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, header.FileInfo().Mode()))

		err := try(func() {
			throw2(io.Copy(file, data))
			throw(file.Chmod(header.FileInfo().Mode()))
		})

		closeErr := file.Close()

		if err != nil {
			err.throw()
		}

		throw(closeErr)
	}
}
