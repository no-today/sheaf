package gitcli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// CommitMessages reads raw commit objects in one process. Missing objects are absent from the result.
func CommitMessages(ctx context.Context, dir string, oids []Oid) (map[Oid]string, error) {
	messages := make(map[Oid]string, len(oids))
	if len(oids) == 0 {
		return messages, nil
	}
	var input strings.Builder
	for _, oid := range oids {
		fmt.Fprintln(&input, oid.String())
	}
	err := stream(ctx, dir, input.String(), func(out io.Reader) error {
		r := bufio.NewReader(out)
		for _, oid := range oids {
			header, err := r.ReadString('\n')
			if err != nil {
				return fmt.Errorf("read commit %s header: %w", oid, err)
			}
			fields := strings.Fields(header)
			if len(fields) == 2 && fields[0] == oid.String() && fields[1] == "missing" {
				continue
			}
			if len(fields) != 3 || fields[0] != oid.String() || fields[1] != "commit" {
				return fmt.Errorf("unexpected commit header %q", header)
			}
			size, err := strconv.Atoi(fields[2])
			if err != nil || size < 0 {
				return fmt.Errorf("invalid commit size %q", fields[2])
			}
			object := make([]byte, size)
			if _, err := io.ReadFull(r, object); err != nil {
				return fmt.Errorf("read commit %s: %w", oid, err)
			}
			if sep, err := r.ReadByte(); err != nil || sep != '\n' {
				return fmt.Errorf("invalid commit %s terminator", oid)
			}
			_, message, ok := strings.Cut(string(object), "\n\n")
			if !ok {
				return fmt.Errorf("commit %s has no message separator", oid)
			}
			messages[oid] = message
		}
		return nil
	}, "cat-file", "--batch")
	return messages, err
}
