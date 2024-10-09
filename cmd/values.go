package cmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

func init() {
	helmvalues.PersistentFlags().StringSliceVarP(&valueFiles, "values", "f", []string{}, "specify override values YAML file (can specify multiple)")
	helmvalues.PersistentFlags().StringVarP(&outfile, "outfile", "o", "", "Output file with merge values (optional, if not supplied - outputs to stdout)")

	rootCmd.AddCommand(helmvalues)
}

var helmvalues = &cobra.Command{
	Use:   "helmvalues [Chart Path]",
	Short: "override and get merged helm values",
	Long:  `Outputs merged helm chart values`,
	Run: func(cmd *cobra.Command, args []string) {

		// Create outFile to write to, if outfile not specified, write to stdout

		// validate flags
		// prepend default values file
		valueFiles = append([]string{"values.yaml"}, valueFiles...)
		chartpath := "."
		if len(args) == 0 {
			if debug == "true" {
				fmt.Println("No Path Argument provided, using current path")
			}
		} else if len(args) == 1 {
			chartpath = filepath.Clean(args[0])
		} else {
			fmt.Println("Error: only 1 argument expected")
			os.Exit(1)
		}
		merged, err := mergeValues(valueFiles, chartpath)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		yamlData, err := yaml.Marshal(&merged)
		if err != nil {
			fmt.Printf("Error while Marshaling. %v", err)
		}

		if len(outfile) > 0 {
			var outFileOpened *os.File
			var outFileOpenedError error
			//fmt.Println("Opening output file...")
			outFileOpened, outFileOpenedError = os.Create(outfile)
			if outFileOpenedError != nil {
				fmt.Println("Error opening outfile: " + outfile)
				fmt.Println(outFileOpenedError)
				os.Exit(1)
			}
			defer outFileOpened.Close()
			outFileOpened.Write(yamlData)
		} else {
			fmt.Println(string(yamlData))
		}

	},
}

// mergeValues merges values from files specified via -f/--values
func mergeValues(valueFiles []string, directory string) (map[string]interface{}, error) {
	base := map[string]interface{}{}

	// User specified a values files via -f/--values
	for _, filePath := range valueFiles {
		currentMap := map[string]interface{}{}
		filePath = filepath.Join(directory, filePath)
		bytes, err := readFile(filePath)
		if err != nil {
			return nil, err
		}

		if err := yaml.Unmarshal(bytes, &currentMap); err != nil {
			return nil, errors.Wrapf(err, "failed to parse %s", filePath)
		}
		// Merge with the previous map
		base = mergeMaps(base, currentMap)
	}

	return base, nil
}

func mergeMaps(a, b map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(a))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if v, ok := v.(map[string]interface{}); ok {
			if bv, ok := out[k]; ok {
				if bv, ok := bv.(map[string]interface{}); ok {
					out[k] = mergeMaps(bv, v)
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}

// readFile load a file from stdin, the local directory, or a remote file with a url.
func readFile(filePath string) ([]byte, error) {
	if strings.TrimSpace(filePath) == "-" {
		return ioutil.ReadAll(os.Stdin)
	}
	return ioutil.ReadFile(filePath)

}
