package git

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// LineRange is a 1-based, inclusive line span.
type LineRange struct {
	Start int
	End   int
}

// FileDiff is every change made to one file between two commits.
type FileDiff struct {
	Path string
	// Removed lines, numbered as in the old version of the file.
	Removed []LineRange
	// Added lines, numbered as in the new version of the file.
	Added []LineRange
}

var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// DiffFiles returns every file that differs between oldRef and newRef with
// the exact lines removed and added. Renames are reported as a deletion plus
// an addition so a file can't escape its old policy by being moved. Files
// with no textual hunks (binary files, mode changes) are still listed, with
// their whole content treated as changed when they are binary.
func DiffFiles(workDir, oldRef, newRef string) ([]FileDiff, error) {
	names, err := gitOutput(workDir, "-c", "core.quotePath=false", "diff", "--no-renames", "--name-only", "-z", oldRef, newRef)
	if err != nil {
		return nil, err
	}

	byPath := map[string]*FileDiff{}
	var order []string
	for _, name := range strings.Split(names, "\x00") {
		if name == "" {
			continue
		}
		byPath[name] = &FileDiff{Path: name}
		order = append(order, name)
	}

	patch, err := gitOutput(workDir, "-c", "core.quotePath=false", "diff", "--no-renames", "--no-color",
		"--no-ext-diff", "--unified=0", "--src-prefix=a/", "--dst-prefix=b/", oldRef, newRef)
	if err != nil {
		return nil, err
	}

	var oldPath, newPath string
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			oldPath, newPath = "", ""
		case strings.HasPrefix(line, "--- "):
			oldPath = strings.TrimPrefix(strings.TrimPrefix(line, "--- "), "a/")
		case strings.HasPrefix(line, "+++ "):
			newPath = strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/")
		case strings.HasPrefix(line, "Binary files "):
			// Binary content can't be checked line by line; flag line 1 so
			// the file is never treated as unchanged.
			for _, p := range []string{oldPath, newPath} {
				if fd := byPath[p]; fd != nil {
					fd.Added = append(fd.Added, LineRange{Start: 1, End: 1})
				}
			}
		case strings.HasPrefix(line, "@@ "):
			m := hunkHeaderRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			path := newPath
			if path == "/dev/null" || path == "" {
				path = oldPath
			}
			fd := byPath[path]
			if fd == nil {
				continue
			}
			if r, ok := hunkRange(m[1], m[2]); ok {
				fd.Removed = append(fd.Removed, r)
			}
			if r, ok := hunkRange(m[3], m[4]); ok {
				fd.Added = append(fd.Added, r)
			}
		}
	}

	out := make([]FileDiff, 0, len(order))
	for _, p := range order {
		out = append(out, *byPath[p])
	}
	return out, nil
}

// hunkRange turns a hunk header's "start,count" pair into a line range; a
// zero count means no lines on that side.
func hunkRange(startStr, countStr string) (LineRange, bool) {
	start, _ := strconv.Atoi(startStr)
	count := 1
	if countStr != "" {
		count, _ = strconv.Atoi(countStr)
	}
	if count == 0 {
		return LineRange{}, false
	}
	return LineRange{Start: start, End: start + count - 1}, true
}

// ReadFilesAt returns the content of every file at ref whose path ends with
// one of suffixes, keyed by repository-relative path.
func ReadFilesAt(workDir, ref string, suffixes ...string) (map[string]string, error) {
	list, err := gitOutput(workDir, "-c", "core.quotePath=false", "ls-tree", "-r", "-z", "--full-tree", ref)
	if err != nil {
		return nil, err
	}

	type entry struct{ path, sha string }
	var entries []entry
	for _, rec := range strings.Split(list, "\x00") {
		// "<mode> <type> <sha>\t<path>"
		meta, p, ok := strings.Cut(rec, "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 || fields[1] != "blob" || !hasAnySuffix(p, suffixes) {
			continue
		}
		entries = append(entries, entry{path: p, sha: fields[2]})
	}

	files := make(map[string]string, len(entries))
	if len(entries) == 0 {
		return files, nil
	}

	// One `git cat-file --batch` process for every blob.
	var stdin bytes.Buffer
	for _, e := range entries {
		stdin.WriteString(e.sha + "\n")
	}
	cmd := exec.Command("git", "cat-file", "--batch")
	cmd.Dir = workDir
	cmd.Stdin = &stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git cat-file failed: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}

	r := bufio.NewReader(&stdout)
	for _, e := range entries {
		header, err := r.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("git cat-file: unexpected end of output")
		}
		fields := strings.Fields(header)
		if len(fields) != 3 {
			return nil, fmt.Errorf("git cat-file: unexpected header %q", header)
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("git cat-file: bad size in %q", header)
		}
		buf := make([]byte, size+1) // content + trailing newline
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, fmt.Errorf("git cat-file: short read for %s", e.path)
		}
		files[e.path] = string(buf[:size])
	}
	return files, nil
}

// EmptyTree is git's well-known empty tree object; diffing against it shows
// every file as added (used for the first ever apply of an environment).
const EmptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

func hasAnySuffix(p string, suffixes []string) bool {
	if len(suffixes) == 0 {
		return true
	}
	for _, s := range suffixes {
		if strings.HasSuffix(p, s) {
			return true
		}
	}
	return false
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s failed: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
