package cmd

/**
This set of functions resolves plain secrets for injection from sealed secrets. It is only meant to work inside reliza-cd context.
*/

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/template"

	"github.com/google/uuid"
)

const (
	ShellToUse = "sh"
	KubectlApp = "tools/kubectl"
)

func shellout(command string) (string, string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.Command(ShellToUse, "-c", command)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	if err != nil {
		fmt.Println("stdout: ", stdout.String(), "stderr: ", stderr.String(), "error: ", err.Error())
	}

	return stdout.String(), stderr.String(), err
}

func createNamespaceIfMissing(namespace string) {
	nsListOut, _, _ := shellout(KubectlApp + " get ns " + namespace + " | wc -l")
	nsListOut = strings.Replace(nsListOut, "\n", "", -1)
	nsListOutInt, err := strconv.Atoi(nsListOut)
	if err != nil {
		fmt.Println(err)
	} else if nsListOutInt < 2 {
		shellout(KubectlApp + " create ns " + namespace)
	}
}

func produceSecretYaml(w io.Writer, secretName string, sealedSecret string, namespace string) {
	secretTmpl :=
		`apiVersion: bitnami.com/v1alpha1
kind: SealedSecret
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
  annotations:
    sealedsecrets.bitnami.com/namespace-wide: "true"
  labels:
    reliza.io/type: cdresource
    reliza.io/name: {{.Name}}
spec:
  encryptedData:
    secret: {{.Secret}}
  template:
    metadata:
      labels:
        reliza.io/name: {{.Name}}
        reliza.io/type: cdresource`

	var secTmplRes SecretTemplateResolver
	secTmplRes.Name = secretName
	secTmplRes.Namespace = namespace
	secTmplRes.Secret = sealedSecret

	tmpl, err := template.New("secrettmpl").Parse(secretTmpl)
	if err != nil {
		panic(err)
	}

	err = tmpl.Execute(w, secTmplRes)
	if err != nil {
		panic(err)
	}
}

func createSecretFile(filePath string) *os.File {
	ecrSecretFile, err := os.Create(filePath)
	if err != nil {
		fmt.Println(err)
	}
	return ecrSecretFile
}

func resolvePlainSecret(sealedSecret string, namespace string) string {
	// generate unique name for secret
	secretName := "plainsecret-" + uuid.New().String()
	secretPath := "workspace/" + secretName + ".yaml"
	secretFile := createSecretFile(secretPath)
	produceSecretYaml(secretFile, secretName, sealedSecret, namespace)
	shellout(KubectlApp + " apply -f " + secretPath)
	secretWaitCmd := "while ! " + KubectlApp + " get secret " + secretName + " -n " + namespace + "; do sleep 1; done"
	shellout(secretWaitCmd)
	plainSecret, _, _ := shellout(KubectlApp + " get secret " + secretName + " -o jsonpath={.data.secret} -n " + namespace + " | base64 -d")
	// cleanup
	shellout(KubectlApp + " delete sealedsecret " + secretName + " -n " + namespace)
	os.Remove(secretPath)
	return plainSecret
}

type SecretTemplateResolver struct {
	Name      string
	Namespace string
	Secret    string
}
