package registry

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strconv"

	"github.com/hoisie/mustache"
)

type ConfigValues []map[string]interface{}

func (c ConfigValues) Len() int      { return len(c) }
func (c ConfigValues) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c ConfigValues) Less(i, j int) bool {
	iLocation, _ := c[i]["location"].(string)
	jLocation, _ := c[j]["location"].(string)
	return iLocation < jLocation
}

type ConfigTemplate struct {
	Template    string `json:"template"`
	Destination string `json:"destination"`
	Mode        string `json:"mode"`

	Services []string `json:"services"`
	Command  []string `json:"command"`

	mode os.FileMode
	data map[string]ConfigValues
}

func LoadConfigTemplate(path string) (*ConfigTemplate, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	c := &ConfigTemplate{mode: 0644}
	if err := json.NewDecoder(file).Decode(&c); err != nil {
		return nil, err
	}

	if len(c.Mode) > 0 {
		mode, err := strconv.ParseInt(c.Mode, 0, 16)
		if err != nil {
			return nil, err
		}
		c.mode = os.FileMode(mode)
	}

	return c, nil
}

func (c *ConfigTemplate) Update(newData map[string]ConfigValues) error {
	if !reflect.DeepEqual(c.data, newData) {
		if err := c.Render(newData); err != nil {
			return err
		}
		c.data = newData
	}
	return nil
}

func (c *ConfigTemplate) Translate(enum *Enumeration) map[string]ConfigValues {
	newData := make(map[string]ConfigValues)
	if enum != nil {
		for _, service := range c.Services {
			var values ConfigValues
			for _, location := range enum.Provides[service] {
				value := map[string]interface{}{
					"location": location.Location,
				}
				value["running"] = location.Running
				u, err := url.Parse(location.Location)
				if err != nil {
					log.Warn("Could not parse location for %s: %s", service, err)
				} else {
					host, port, err := net.SplitHostPort(u.Host)
					if err != nil {
						// no port
						host = u.Host
					}
					value["scheme"] = u.Scheme
					value["host"] = host
					value["port"] = port
				}
				values = append(values, value)
			}
			// sort entries for subsequent equality checks
			sort.Sort(values)
			newData[service] = values
		}
	}
	return newData
}

func (c *ConfigTemplate) Render(data interface{}) error {
	if log.IsDebugEnabled() {
		log.Debug("Template context for %s: %+v", c.Template, data)
	}

	template, err := ioutil.ReadFile(c.Template)
	if err != nil {
		return err
	}

	renderedTemplate := mustache.Render(string(template), data)
	if log.IsTraceEnabled() {
		log.Trace("Rendered template: %s", renderedTemplate)
	}

	if err := ioutil.WriteFile(c.Destination, []byte(renderedTemplate), c.mode); err != nil {
		return err
	}

	log.Info("Rendered %s -> %s", c.Template, c.Destination)

	if len(c.Command) > 0 {
		cmd := exec.Command(c.Command[0], c.Command[1:]...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("command %s for template %s failed (%s): %s", c.Command, c.Template, err, string(output))
		}
		log.Info("Command %s for template %s succeeded: %s", c.Command, c.Template, string(output))
	}

	return nil
}
