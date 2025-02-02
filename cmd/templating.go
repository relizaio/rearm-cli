package cmd

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	"github.com/machinebox/graphql"
)

func getBundleVersionCycloneDxExportV1(apiKeyId string, apiKey string, bundle string,
	environment string, version string) []byte {

	if len(bundle) <= 0 && (len(version) <= 0 || len(environment) <= 0) {
		//throw error and exit
		fmt.Println("Error: Bundle name and either version or environment must be provided!")
		os.Exit(1)
	}

	client := graphql.NewClient(rearmUri + "/graphql")
	req := graphql.NewRequest(`
		query ($bundleName: String!, $bundleVersion: String, $environment: String) {
			exportAsBomProg(bundleName: $bundleName, bundleVersion: $bundleVersion, environment: $environment)
		}
	`)
	req.Var("bundleName", bundle)
	req.Var("bundleVersion", version)
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
