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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	instEventDeployment    string
	instEventPhase         string
	instEventFailureClass  string
	instEventMessage       string
	instEventDetail        string
	instEventDetailFile    string
	instEventFingerprint   string
	instEventFeatureSet    string
	instEventProduct       string
	instEventTargetRelease string
	instEventAttemptedAt   string
	instEventsJson         string
	instEventsFile         string
)

// maxDetailChars bounds what we transmit. The server clamps again at a higher
// limit; this is the agent-side bound so a runaway helm dump never leaves the
// cluster in the first place.
const maxDetailChars = 4096

// maxMessageChars bounds the one-line summary.
const maxMessageChars = 1024

// secretPatterns redact obvious credential material out of captured stderr
// BEFORE it leaves the cluster. Helm output routinely echoes rendered values,
// and values files carry tokens/passwords -- shipping raw stderr to a hosted
// backend would be a credential-exfiltration path. Redaction is deliberately
// aggressive: a lost diagnostic line is cheap, a leaked secret is not.
var secretPatterns = []*regexp.Regexp{
	// key: value / key=value where the key CONTAINS a credential word.
	// The credential word is matched as a substring on purpose: helm values are
	// overwhelmingly camelCase (adminPassword, registryToken, dbSecretKey), and
	// an anchored \bpassword\b misses every one of them.
	regexp.MustCompile(`(?i)[\w.-]*(password|passwd|secret|token|api[_-]?key|apikey|access[_-]?key|private[_-]?key|credential)[\w.-]*\s*[:=]\s*"?[^\s",}]+"?`),
	// Short/ambiguous keys, whole-word only — "pwd" and "auth" as substrings
	// would maul ordinary prose ("author", "authority", "forward").
	regexp.MustCompile(`(?i)\b(pwd|auth)\b\s*[:=]\s*"?[^\s",}]+"?`),
	// Authorization headers (Bearer/Basic)
	regexp.MustCompile(`(?i)\bauthorization\b\s*[:=]\s*"?(bearer|basic)?\s*[A-Za-z0-9._~+/=-]+"?`),
	// PEM blocks
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
	// JWTs
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\b`),
	// credentials embedded in URLs
	regexp.MustCompile(`\b([a-zA-Z][a-zA-Z0-9+.-]*://)[^\s/:@]+:[^\s/@]+@`),
}

// volatilePatterns are stripped only when deriving a fingerprint, so the same
// underlying fault yields one stable dedup key instead of a new row per
// reconcile. They are NOT removed from the transmitted message/detail.
var volatilePatterns = []*regexp.Regexp{
	regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`), // uuids
	regexp.MustCompile(`(?i)\bsha256:[0-9a-f]{8,64}\b`),                                                // digests
	regexp.MustCompile(`(?i)\b\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}\S*`),                              // timestamps
	regexp.MustCompile(`\b\d+\b`),                                                                      // counters, ports, epochs
}

// redactSecrets replaces credential-looking material with a marker.
func redactSecrets(s string) string {
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, "[REDACTED]")
	}
	return s
}

// truncateChars clamps to max runes, marking that it happened so an operator
// knows the tail is missing rather than assuming a short error.
func truncateChars(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "\n…[truncated]"
}

// sanitizeDetail is the single path everything captured from a subprocess must
// go through before transmit.
func sanitizeDetail(s string) string {
	return truncateChars(redactSecrets(s), maxDetailChars)
}

// deriveFailureFingerprint produces a stable dedup key for a fault. Volatile tokens
// are normalised out so a message that embeds a uuid or timestamp does not
// create a fresh row on every reconcile.
func deriveFailureFingerprint(deploymentName, phase, failureClass, message string) string {
	// Normalise BEFORE lowercasing: several volatile patterns are anchored on
	// case-bearing syntax (the T in an RFC3339 timestamp), and lowercasing
	// first silently defeated them — which let timestamps through and made a
	// persistent failure insert a fresh row on every reconcile.
	norm := message
	for _, re := range volatilePatterns {
		norm = re.ReplaceAllString(norm, "*")
	}
	norm = strings.ToLower(norm)
	norm = strings.Join(strings.Fields(norm), " ")
	sum := sha256.Sum256([]byte(deploymentName + "|" + phase + "|" + failureClass + "|" + norm))
	return hex.EncodeToString(sum[:16])
}

// buildInstEvent assembles one event from flags, applying redaction,
// truncation and fingerprint derivation.
func buildInstEvent() (map[string]interface{}, error) {
	if instEventDeployment == "" {
		return nil, fmt.Errorf("--deployment is required")
	}
	detail := instEventDetail
	if instEventDetailFile != "" {
		b, err := os.ReadFile(instEventDetailFile)
		if err != nil {
			return nil, fmt.Errorf("cannot read --detailfile: %w", err)
		}
		detail = string(b)
	}
	phase := strings.ToUpper(instEventPhase)
	if phase == "" {
		phase = "UNKNOWN"
	}
	failureClass := strings.ToUpper(strings.ReplaceAll(instEventFailureClass, "-", "_"))
	if failureClass == "" {
		failureClass = "UNKNOWN"
	}
	message := truncateChars(redactSecrets(instEventMessage), maxMessageChars)
	fingerprint := instEventFingerprint
	if fingerprint == "" {
		fingerprint = deriveFailureFingerprint(instEventDeployment, phase, failureClass, instEventMessage)
	}
	attemptedAt := instEventAttemptedAt
	if attemptedAt == "" {
		attemptedAt = time.Now().UTC().Format(time.RFC3339)
	}

	e := map[string]interface{}{
		"deploymentName": instEventDeployment,
		"phase":          phase,
		"failureClass":   failureClass,
		"fingerprint":    fingerprint,
		"attemptedAt":    attemptedAt,
	}
	if message != "" {
		e["message"] = message
	}
	if detail != "" {
		e["detail"] = sanitizeDetail(detail)
	}
	if namespace != "" {
		e["namespace"] = namespace
	}
	if senderId != "" {
		e["senderId"] = senderId
	}
	if instEventFeatureSet != "" {
		e["featureSet"] = instEventFeatureSet
	}
	if instEventProduct != "" {
		e["product"] = instEventProduct
	}
	if instEventTargetRelease != "" {
		e["targetRelease"] = instEventTargetRelease
	}
	return e, nil
}

// sanitizeEventBatch applies the same redaction/truncation guarantees to a
// caller-supplied JSON batch, so the batch path cannot bypass them.
func sanitizeEventBatch(raw []byte) ([]map[string]interface{}, error) {
	var events []map[string]interface{}
	if err := json.Unmarshal(raw, &events); err != nil {
		return nil, fmt.Errorf("events must be a JSON array of objects: %w", err)
	}
	for i, e := range events {
		if name, _ := e["deploymentName"].(string); name == "" {
			return nil, fmt.Errorf("event %d is missing deploymentName", i)
		}
		if d, ok := e["detail"].(string); ok {
			e["detail"] = sanitizeDetail(d)
		}
		if m, ok := e["message"].(string); ok {
			e["message"] = truncateChars(redactSecrets(m), maxMessageChars)
		}
		if p, ok := e["phase"].(string); ok {
			e["phase"] = strings.ToUpper(p)
		} else {
			e["phase"] = "UNKNOWN"
		}
		if fc, ok := e["failureClass"].(string); ok {
			e["failureClass"] = strings.ToUpper(strings.ReplaceAll(fc, "-", "_"))
		} else {
			e["failureClass"] = "UNKNOWN"
		}
		if fp, _ := e["fingerprint"].(string); fp == "" {
			msg, _ := e["message"].(string)
			e["fingerprint"] = deriveFailureFingerprint(
				e["deploymentName"].(string), e["phase"].(string), e["failureClass"].(string), msg)
		}
		if at, _ := e["attemptedAt"].(string); at == "" {
			e["attemptedAt"] = time.Now().UTC().Format(time.RFC3339)
		}
	}
	return events, nil
}

func init() {
	instEventCmd.PersistentFlags().StringVar(&instEventDeployment, "deployment", "", "Deployment name that failed (required unless --events/--eventsfile is used)")
	instEventCmd.PersistentFlags().StringVar(&namespace, "namespace", "", "Namespace of the deployment (recommended; required when using a CLUSTER-scoped api key)")
	instEventCmd.PersistentFlags().StringVar(&senderId, "sender", "", "Unique sender within a single namespace (optional)")
	instEventCmd.PersistentFlags().StringVar(&instEventPhase, "phase", "", "Reconcile phase: VALUES_MERGE, TAG_REPLACE, SECRETS, HELM_INSTALL, HELM_UNINSTALL, WATCHER_INSTALL, BACKUP (optional, default UNKNOWN)")
	instEventCmd.PersistentFlags().StringVar(&instEventFailureClass, "failureclass", "", "Failure class: RBAC_FORBIDDEN, CHART_NOT_FOUND, TIMEOUT, IMAGE_PULL, VALUES_INVALID, PRECONDITION_MISSING (optional, default UNKNOWN)")
	instEventCmd.PersistentFlags().StringVar(&instEventMessage, "message", "", "One-line failure summary (optional)")
	instEventCmd.PersistentFlags().StringVar(&instEventDetail, "detail", "", "Failure detail, e.g. captured stderr (optional; redacted and truncated before transmit)")
	instEventCmd.PersistentFlags().StringVar(&instEventDetailFile, "detailfile", "", "Path to a file with failure detail (optional; redacted and truncated before transmit)")
	instEventCmd.PersistentFlags().StringVar(&instEventFingerprint, "fingerprint", "", "Stable dedup key for this fault (optional, derived from deployment+phase+class+normalized message when omitted)")
	instEventCmd.PersistentFlags().StringVar(&instEventFeatureSet, "featureset", "", "Feature set UUID (optional)")
	instEventCmd.PersistentFlags().StringVar(&instEventProduct, "product", "", "Product UUID (optional)")
	instEventCmd.PersistentFlags().StringVar(&instEventTargetRelease, "targetrelease", "", "Target release UUID (optional)")
	instEventCmd.PersistentFlags().StringVar(&instEventAttemptedAt, "attemptedat", "", "RFC3339 time of the failed attempt (optional, defaults to now)")
	instEventCmd.PersistentFlags().StringVar(&instEventsJson, "events", "", "JSON array of event objects for batch reporting (optional, mutually exclusive with single-event flags)")
	instEventCmd.PersistentFlags().StringVar(&instEventsFile, "eventsfile", "", "Path to a file with the JSON array of event objects (optional)")

	devopsCmd.AddCommand(instEventCmd)
}

var instEventCmd = &cobra.Command{
	Use:   "instevent",
	Short: "Reports deployment failures from a CD agent to ReARM",
	Long: `Reports deployment failure events for an instance, surfaced in the ReARM Instance view.

Events are deduplicated server-side on (namespace, deploymentName, fingerprint):
a failure that repeats on every reconcile updates one record's last-seen time and
occurrence count rather than creating new rows. Supply a stable --fingerprint when
the agent can compute one; otherwise it is derived from the deployment, phase,
failure class and a normalized message.

Failure detail is redacted (credential-looking material) and truncated before it
leaves the cluster -- helm output can echo rendered values.

Batch usage (preferred for a reconcile loop):
  rearm devops instevent --eventsfile ./events.json

Single event:
  rearm devops instevent --deployment traefik --namespace traefik \
    --phase HELM_INSTALL --failureclass RBAC_FORBIDDEN \
    --message "UPGRADE FAILED: could not get clusterroles" --detailfile ./helm-stderr.txt`,
	Run: func(cmd *cobra.Command, args []string) {
		if debug == "true" {
			fmt.Println("Using ReARM at", rearmUri)
		}

		var events []map[string]interface{}
		var err error
		switch {
		case instEventsJson != "":
			events, err = sanitizeEventBatch([]byte(instEventsJson))
		case instEventsFile != "":
			var raw []byte
			raw, err = os.ReadFile(instEventsFile)
			if err == nil {
				events, err = sanitizeEventBatch(raw)
			}
		default:
			var e map[string]interface{}
			e, err = buildInstEvent()
			if err == nil {
				events = []map[string]interface{}{e}
			}
		}
		if err != nil {
			fmt.Println("Error preparing deployment events:", err)
			os.Exit(1)
		}
		if len(events) == 0 {
			if debug == "true" {
				fmt.Println("No deployment events to send")
			}
			return
		}

		if debug == "true" {
			fmt.Println(events)
		}

		query := `
			mutation ($events: [InstanceDeploymentEventInput!]!) {
				instanceDeploymentEventsProgrammatic(events:$events)
			}
		`
		variables := map[string]interface{}{"events": events}
		fmt.Println(sendRequest(query, variables, "instanceDeploymentEventsProgrammatic"))
	},
}
