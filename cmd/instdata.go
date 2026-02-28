/*
The MIT License (MIT)

Copyright (c) 2020 - 2026 Reliza Incorporated (Reliza (tm), https://reliza.io)

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"),
to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense,
and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

*/

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/machinebox/graphql"
	"github.com/spf13/cobra"
)

var imageString string
var imageFilePath string
var imageStyle string
var senderId string

func init() {
	instDataCmd.PersistentFlags().StringVar(&imageString, "images", "", "Whitespace-separated sha256 digests of images from the instance (optional, either images or imagefile must be provided)")
	instDataCmd.PersistentFlags().StringVar(&imageFilePath, "imagefile", "/resources/images", "Absolute path to file with image string or image k8s json (optional, either images or imagefile must be provided)")
	instDataCmd.PersistentFlags().StringVar(&imageStyle, "imagestyle", "", "Set to 'k8s' for k8s json image format (optional)")
	instDataCmd.PersistentFlags().StringVar(&namespace, "namespace", "", "Namespace where images are being sent (optional, defaults to 'default')")
	instDataCmd.PersistentFlags().StringVar(&senderId, "sender", "", "Unique sender within a single namespace (optional)")

	devopsCmd.AddCommand(instDataCmd)
}

var instDataCmd = &cobra.Command{
	Use:   "instdata",
	Short: "Sends instance data to ReARM",
	Long:  `This CLI command would stream agent data from instance to ReARM`,
	Run: func(cmd *cobra.Command, args []string) {
		if debug == "true" {
			fmt.Println("Using ReARM at", rearmUri)
		}

		body := map[string]interface{}{}
		// if imageString (--images flag) is supplied, image File path is ignored
		if imageString != "" {
			// only non-k8s images supported
			body["images"] = strings.Fields(imageString)
		} else {
			imageBytes, err := os.ReadFile(imageFilePath)
			if err != nil {
				fmt.Println("Error when reading images file")
				fmt.Print(err)
				os.Exit(1)
			}
			if imageStyle == "k8s" {
				var k8sjson []map[string]interface{}
				errJson := json.Unmarshal(imageBytes, &k8sjson)
				if errJson != nil {
					fmt.Println("Error unmarshalling k8s images")
					fmt.Println(errJson)
					os.Exit(1)
				}
				body["type"] = "k8s"
				body["images"] = k8sjson
			} else {
				body["images"] = strings.Fields(string(imageBytes))
			}
		}
		body["timeSent"] = time.Now().UTC().Format(time.RFC3339)
		if len(namespace) > 0 {
			body["namespace"] = namespace
		}
		if len(senderId) > 0 {
			body["senderId"] = senderId
		}

		if debug == "true" {
			fmt.Println(body)
		}

		req := graphql.NewRequest(`
			mutation ($InstanceDataInput: InstanceDataInput!) {
				instData(instance:$InstanceDataInput)
			}
		`)
		req.Var("InstanceDataInput", body)
		fmt.Println(sendRequest(req, "instData"))
	},
}
