package agent

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/catalyzeio/go-core/comm"
)

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	return true, err
}

func invoke(name string, args ...string) error {
	start := time.Now()

	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()

	duration := time.Now().Sub(start)

	if err != nil {
		err = fmt.Errorf("command {%s %s} failed in %s (%s): %s", name, args, duration, err, string(output))
	} else {
		if log.IsDebugEnabled() {
			log.Debug("Command {%s %s} succeeded in %s: %s", name, args, duration, string(output))
		}
	}
	return err
}

// Replaces any invalid characters for the environment variable name with "_",
// and ensures the name starts with an alpha or underscore character.
func quoteEnvName(name string) string {
	if len(name) == 0 {
		return ""
	}
	escaped := comm.SanitizeService(name)
	c := escaped[0]
	if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' {
		return escaped
	}
	return "_" + escaped
}

// Surrounds the value with single quotes,
// and escapes any single quotes for use with shell scripts.
// This is a dirty (but correct) version of Python's shlex.quote.
func quoteEnvValue(value string) string {
	return fmt.Sprintf("'%s'", strings.Replace(value, `'`, `'"'"'`, -1))
}

func fileToString(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := bytes.NewBuffer(nil)
	_, err = io.Copy(buf, f)
	if err != nil {
		return "", err
	}
	return string(buf.Bytes()), nil
}
