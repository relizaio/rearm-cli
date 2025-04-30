/*
The MIT License (MIT)

Copyright (c) 2020 - 2025 Reliza Incorporated (Reliza (tm), https://reliza.io)

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

	cdx "github.com/CycloneDX/cyclonedx-go"
	"github.com/machinebox/graphql"
	"github.com/spf13/cobra"
)

var (
	bearUri string
)

var resolveBearSupplierCmd = &cobra.Command{
	Use:   "enrichsupplier",
	Short: "Resolve Supplier on BEAR",
	Long:  `For each component, resolve its supplier where not present on given BEAR instance`,
	Run: func(cmd *cobra.Command, args []string) {
		resolveBearSupplierFunc()
	},
}

func init() {
	resolveBearSupplierCmd.PersistentFlags().StringVar(&bearUri, "bearUri", "https://beardemo.rearmhq.com", "BEAR URI to use")
	bomUtils.AddCommand(resolveBearSupplierCmd)
}

func resolveBearSupplierFunc() {
	fmt.Println(bearUri)

	bom, err := readBom()

	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	components := bom.Components
	fmt.Println(len(*components))
	for _, comp := range *components {
		if nil == comp.Supplier {
			supplier := bearGqlSupplierRoutine(comp.PackageURL)
			comp.Supplier = &supplier
		}
	}

	outErr := writeOutput(bom)
	if outErr != nil {
		fmt.Println(outErr)
		os.Exit(1)
	}
}

func bearGqlSupplierRoutine(purl string) cdx.OrganizationalEntity {
	req := graphql.NewRequest(`
		mutation resolveSupplier($purl: String!) {
			resolveSupplier(purl: $purl) {
				name
				address {
					country
					region
					locality
					postOfficeBoxNumber
					postalCode
					streetAddress
				}
				url
				contact {
					name
					email
					phone
				}
			}
		}
	`)
	req.Var("purl", purl)
	fmt.Println("adding bom...")
	res := sendRequestWithUri(req, "resolveSupplier", bearUri+"/graphql")
	fmt.Println(res)
	// var bomJSON map[string]interface{}
	var cdxContact cdx.OrganizationalEntity
	parseError := json.Unmarshal([]byte(res), &cdxContact)
	if parseError != nil {
		fmt.Println(parseError)
		os.Exit(1)
	}
	return cdxContact
}
