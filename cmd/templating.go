package cmd

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	"github.com/machinebox/graphql"
)

func getProductVersionCycloneDxExportV1(apiKeyId string, apiKey string, product string,
	environment string, version string) []byte {

	if len(product) <= 0 && (len(version) <= 0 || len(environment) <= 0) {
		//throw error and exit
		fmt.Println("Error: Product name and either version or environment must be provided!")
		os.Exit(1)
	}

	client := graphql.NewClient(rearmUri + "/graphql")
	req := graphql.NewRequest(`
		query ($productName: String!, $productVersion: String, $environment: String) {
			exportAsBomProg(productName: $productName, productVersion: $productVersion, environment: $environment)
		}
	`)
	req.Var("productName", product)
	req.Var("productVersion", version)
	req.Var("environment", environment)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Reliza Go Client")
	req.Header.Set("Accept-Encoding", "gzip, deflate")

	if len(apiKeyId) > 0 && len(apiKey) > 0 {
		auth := base64.StdEncoding.EncodeToString([]byte(apiKeyId + ":" + apiKey))
		req.Header.Add("Authorization", "Basic "+auth)
	}
	session, _ := getSession()
	if session != nil {
		req.Header.Set("X-CSRF-Token", session.Token)
		req.Header.Set("Cookie", "JSESSIONID="+session.JSessionId)
	}

	var respData map[string]string
	if err := client.Run(context.Background(), req, &respData); err != nil {
		printGqlError(err)
		os.Exit(1)
	}

	return []byte(respData["exportAsBomProg"])
}
