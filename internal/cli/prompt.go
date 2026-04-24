package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// confirm reads a yes/no response from r after writing prompt to w.
// The default is No: empty input, EOF, and anything other than a
// case-insensitive "y" / "yes" return false. Taking the reader and
// writer as parameters keeps the helper testable without touching
// os.Stdin or os.Stderr.
func confirm(r io.Reader, w io.Writer, prompt string) (bool, error) {
	fmt.Fprintf(w, "%s ", prompt)
	buf := bufio.NewReader(r)
	line, err := buf.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes", nil
}
