package registry

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	// setup the template file
	templatePath, err := writeTempFile(`code:
{{#code}}
  {{{host}}}:{{{port}}}{{^running}} down{{/running}}
{{/code}}
logging:
{{#logging}}
  {{{host}}}:{{{port}}}{{^running}} down{{/running}}
{{/logging}}`)
	if err != nil {
		t.Fatalf("Failed to create a temp file: %s", err)
	}
	defer os.Remove(templatePath)

	renderPath := fmt.Sprintf("%s.out", templatePath)
	defer os.Remove(renderPath)

	// setup the json file
	jsonPath, err := writeTempFile(fmt.Sprintf(`{
  "template": "%s",
  "destination": "%s",
  "services": [
    "code", "logging"
  ],
  "command": [
    "echo"
  ]
}`, templatePath, renderPath))
	if err != nil {
		t.Fatalf("Failed to create a temp file: %s", err)
	}
	defer os.Remove(jsonPath)

	// construct an Enumeration and perform the file rendering
	c, err := LoadConfigTemplate(jsonPath)
	if err != nil {
		t.Fatalf("Failed to load the JSON file: %s", err)
	}

	enum := &Enumeration{
		Provides: map[string][]WeightedLocation{
			"code": []WeightedLocation{
				WeightedLocation{Location: "http://127.0.0.1:8080", Weight: 0.5, Running: true},
				WeightedLocation{Location: "http://127.0.0.1:8081", Weight: 0.5, Running: false},
			},
			"logging": []WeightedLocation{
				WeightedLocation{Location: "http://127.0.0.1:8082", Weight: 1.0, Running: true},
			},
		},
	}
	err = c.Update(c.Translate(enum))
	if err != nil {
		t.Fatalf("Failed to render the template file: %s", err)
	}

	data, err := ioutil.ReadFile(renderPath)
	if err != nil {
		t.Fatalf("Failed to read the rendered file: %s", err)
	}

	expected := `code:
  127.0.0.1:8080
  127.0.0.1:8081 down

logging:
  127.0.0.1:8082`
	if strings.TrimSpace(string(data)) != expected {
		t.Fatalf("Unexpected data. Expected:\n%s\n\nActual:\n%s", expected, data)
	}
}

func writeTempFile(data string) (string, error) {
	tmpFile, err := ioutil.TempFile("", "test-render")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()
	_, err = tmpFile.WriteString(data)
	if err != nil {
		return "", err
	}
	return tmpFile.Name(), nil
}
