/*
The MIT License (MIT)

Copyright (c) 2019 - 2026 Reliza Incorporated. https://reliza.io
*/

package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	rearm "github.com/relizaio/rearm-client-go"
)

// The import check of VEX and SAFETY_CONTEXT uploads (task SAFE-16, design SAFE-9 3.4 to 3.8).
//
// addartifact reads the import summary the server stored on each VEX or SAFETY_CONTEXT artifact of
// this call from the mutation's own answer, prints one block per such artifact on stderr, and exits
// 3 when one imported nothing or has no known result, unless --allow-import-failure is on the
// command line. Entries whose result the server never returns (nested under another artifact, or on
// a source code entry) are refused before any request unless that flag is given. addrelease,
// addreleases, addodeliverable and getversion never import such an artifact and refuse it the same
// way.

// allowImportFailureFlag is the name of the opt-out flag on the five commands. bindFlags skips it,
// so neither REARM_ALLOW_IMPORT_FAILURE nor a config file key sets it (design d14).
const allowImportFailureFlag = "allow-import-failure"

// waitForSbomFlag is the name of addartifact's retry flag; it keeps the environment and config binding.
const waitForSbomFlag = "wait-for-sbom"

// importCheckTagKey is the tag the CLI adds to each checked entry, with a fresh random value, to find
// that entry's own artifact in the answer (design d13).
const importCheckTagKey = "rearmCliImportCheck"

// sbomNotIndexedMarker is the text of the server's answer to an import before the release's SBOM is
// indexed; nothing is stored when it is refused this way (design d8).
const sbomNotIndexedMarker = "release has no SBOM components yet"

// noFindingAnalysisWriteReason is the demotion reason of a statement staged because the uploading
// key does not hold Finding Analysis write at the import scope.
const noFindingAnalysisWriteReason = "NO_FINDING_ANALYSIS_WRITE"

// overrideOutcomeNone is the override outcome of an upload without an issuer class override.
const overrideOutcomeNone = "NONE"

// Artifact types whose upload runs an import, compared exactly (the server's enum is case-sensitive).
const (
	artifactTypeVex           = "VEX"
	artifactTypeSafetyContext = "SAFETY_CONTEXT"
)

func isImportableType(artifactType string) bool {
	return artifactType == artifactTypeVex || artifactType == artifactTypeSafetyContext
}

// Limits of what the summary prints from the server or the user (design 3.6).
const (
	maxRenderedRunes    = 300
	maxRenderedKeyRunes = 100
	maxPrintedMessages  = 20
)

// Help texts shared by the five commands (design 3.10).
const (
	allowImportFailureReadOnlyCommandLine = "read only from the command line: REARM_ALLOW_IMPORT_FAILURE and the config file do not set it"
	allowImportFailureAddArtifactHelp     = "exit 0 even when a VEX or SAFETY_CONTEXT artifact imported nothing or its result is unknown (the summary is still printed); also uploads such an artifact on a source code entry or nested under another artifact, unchecked; " + allowImportFailureReadOnlyCommandLine
	allowImportFailureStoreOnlyHelp       = "send VEX and SAFETY_CONTEXT artifacts although this command never imports them (they are only stored); " + allowImportFailureReadOnlyCommandLine
	nonImportingCommandLongHelp           = "A VEX or SAFETY_CONTEXT artifact is refused: this command stores it but never imports it. Upload it with addartifact once the release's SBOM is indexed."
	waitForSbomHelp                       = "retry the upload while the release has no indexed SBOM, for at most this long (e.g. 10m); 0 does not wait"
)

var (
	// allowImportFailure is written only by cobra's parsing of the command line (design d14).
	allowImportFailure bool
	// waitForSbom is addartifact's --wait-for-sbom.
	waitForSbom time.Duration

	// importSleep and importNow are the clock of the --wait-for-sbom retry; tests replace them.
	importSleep = time.Sleep
	importNow   = time.Now
	// importCheckRandom is the source of the check tag values: crypto/rand.
	importCheckRandom io.Reader = rand.Reader
)

// sbomWaitBackoff is the wait before each retry; the last value repeats.
var sbomWaitBackoff = []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second}

// ---------------------------------------------------------------------------------------------
// addartifact input (design 3.4)

// artifactPart is the part of addartifact's input an entry belongs to.
type artifactPart int

const (
	partRelease artifactPart = iota
	partDeliverable
	partSce
)

// importEntry is one VEX or SAFETY_CONTEXT entry of addartifact's input, at any depth and in any part.
type importEntry struct {
	ref         string
	artType     string
	displayId   string
	part        artifactPart
	nested      bool
	deliverable string // the group's deliverable, for a deliverable entry
	checkValue  string // the check tag value, on a checked entry
	art         *Artifact
}

// checked: the server returns this entry's result (top level of the release part or of a
// deliverable group, design 3.5).
func (e *importEntry) checked() bool {
	return !e.nested && (e.part == partRelease || e.part == partDeliverable)
}

// addArtifactInput is addartifact's decoded and checked input.
type addArtifactInput struct {
	allowFailure      bool
	releaseArts       []Artifact
	hasRelease        bool
	deliverableGroups []DeliverableArtifactGroup
	hasDeliverable    bool
	sceGroups         []SceArtifactGroup
	hasSce            bool
	// importable entries in input order: release part, deliverable groups, source code entry
	// groups; within a part, list order with a nested entry right after its parent
	importable []*importEntry
	// deprecation notices, printed only when every check passed
	notices []string
}

// legacyInputError is a JSON error printed as addartifact always printed it (on stdout).
type legacyInputError struct{ line string }

func (e *legacyInputError) Error() string { return e.line }

// Known keys: the JSON names encoding/json binds for each struct, so the list cannot drift from it.
var (
	artifactKnownKeys         = jsonTagNames(reflect.TypeOf(Artifact{}))
	deliverableGroupKnownKeys = jsonTagNames(reflect.TypeOf(DeliverableArtifactGroup{}))
	sceGroupKnownKeys         = jsonTagNames(reflect.TypeOf(SceArtifactGroup{}))
)

// jsonTagNames is the sorted JSON names of a struct's fields.
func jsonTagNames(t reflect.Type) []string {
	names := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		name := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// decodeAddArtifactInput decodes and checks addartifact's artifact flags before any request
// (design 3.4 items 2, 4, 5, 8, 9). releaseFlag is --artifacts or --releasearts, whichever carries
// the release part. An error is either a *legacyInputError (JSON that does not decode) or a line
// for "Error: " on stderr.
func decodeAddArtifactInput(releaseFlag, releaseJSON, deliverableJSON, sceJSON string, allowFailure bool) (*addArtifactInput, error) {
	in := &addArtifactInput{allowFailure: allowFailure}
	if releaseJSON != "" {
		in.hasRelease = true
		if err := json.Unmarshal([]byte(releaseJSON), &in.releaseArts); err != nil {
			return nil, &legacyInputError{fmt.Sprintf("Error parsing releasearts JSON: %v", err)}
		}
		var raws []map[string]json.RawMessage
		_ = json.Unmarshal([]byte(releaseJSON), &raws)
		if err := in.walkArtifacts(releaseFlag, raws, in.releaseArts, partRelease, false, ""); err != nil {
			return nil, err
		}
	}
	if deliverableJSON != "" {
		in.hasDeliverable = true
		if err := json.Unmarshal([]byte(deliverableJSON), &in.deliverableGroups); err != nil {
			return nil, &legacyInputError{fmt.Sprintf("Error parsing deliverablearts JSON: %v", err)}
		}
		var raws []map[string]json.RawMessage
		_ = json.Unmarshal([]byte(deliverableJSON), &raws)
		for i := range in.deliverableGroups {
			ref := fmt.Sprintf("--deliverablearts[%d]", i)
			raw := rawAt(raws, i)
			if err := checkKnownKeys(ref, raw, deliverableGroupKnownKeys); err != nil {
				return nil, err
			}
			g := &in.deliverableGroups[i]
			if err := in.walkArtifacts(ref+".artifacts", rawChildren(raw), g.Artifacts, partDeliverable, false, g.Deliverable); err != nil {
				return nil, err
			}
		}
	}
	if sceJSON != "" {
		in.hasSce = true
		if err := json.Unmarshal([]byte(sceJSON), &in.sceGroups); err != nil {
			return nil, &legacyInputError{fmt.Sprintf("Error parsing scearts JSON: %v", err)}
		}
		var raws []map[string]json.RawMessage
		_ = json.Unmarshal([]byte(sceJSON), &raws)
		for i := range in.sceGroups {
			ref := fmt.Sprintf("--scearts[%d]", i)
			raw := rawAt(raws, i)
			if err := checkKnownKeys(ref, raw, sceGroupKnownKeys); err != nil {
				return nil, err
			}
			if err := in.walkArtifacts(ref+".artifacts", rawChildren(raw), in.sceGroups[i].Artifacts, partSce, false, ""); err != nil {
				return nil, err
			}
		}
	}
	return in, nil
}

// walkArtifacts checks each artifact of a list in walk order (list order, a nested entry right
// after its parent): unknown keys, a nested importable entry, a top-level importable entry on a
// source code entry, a digest scope computed by ReARM; and records importable entries and notices.
func (in *addArtifactInput) walkArtifacts(ref string, raws []map[string]json.RawMessage, arts []Artifact, part artifactPart, nested bool, deliverable string) error {
	for i := range arts {
		r := fmt.Sprintf("%s[%d]", ref, i)
		raw := rawAt(raws, i)
		if err := checkKnownKeys(r, raw, artifactKnownKeys); err != nil {
			return err
		}
		a := &arts[i]
		importable := isImportableType(a.Type)
		if importable && nested && !in.allowFailure {
			return fmt.Errorf("%s: a VEX or SAFETY_CONTEXT artifact nested under another artifact is stored but never imported; list it at the top level, or pass --%s to upload it anyway", r, allowImportFailureFlag)
		}
		if importable && !nested && part == partSce && !in.allowFailure {
			return fmt.Errorf("%s: the server does not report the import result of a VEX or SAFETY_CONTEXT artifact on a source code entry, so this CLI cannot check it; upload it with --releasearts (the import runs against the release either way), or pass --%s to upload it unchecked", r, allowImportFailureFlag)
		}
		for _, d := range a.DigestRecords {
			if scope := strings.ToUpper(d.Scope); serverDerivedDigestScopes[scope] {
				return fmt.Errorf("%s: digest scope %s is computed by ReARM and cannot be supplied", r, scope)
			}
		}
		in.notices = append(in.notices, deprecatedAliasNotices(r, a)...)
		if importable {
			in.importable = append(in.importable, &importEntry{
				ref: r, artType: a.Type, displayId: a.DisplayIdentifier, part: part, nested: nested,
				deliverable: deliverable, art: a,
			})
		}
		if err := in.walkArtifacts(r+".artifacts", rawChildren(raw), a.Artifacts, part, true, deliverable); err != nil {
			return err
		}
	}
	return nil
}

func rawAt(raws []map[string]json.RawMessage, i int) map[string]json.RawMessage {
	if i < len(raws) {
		return raws[i]
	}
	return nil
}

// rawChildren is the nested artifact list of a raw object, under the key encoding/json binds to
// "artifacts" (ignoring case).
func rawChildren(raw map[string]json.RawMessage) []map[string]json.RawMessage {
	var children []map[string]json.RawMessage
	for _, k := range sortedKeys(raw) {
		if strings.EqualFold(k, "artifacts") {
			children = nil
			_ = json.Unmarshal(raw[k], &children)
		}
	}
	return children
}

func sortedKeys(raw map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// checkKnownKeys refuses the first key, in sorted order, that is not a known name ignoring case
// (the rule encoding/json binds keys by).
func checkKnownKeys(ref string, raw map[string]json.RawMessage, known []string) error {
	for _, k := range sortedKeys(raw) {
		if !isKnownKey(k, known) {
			return fmt.Errorf("%s: unknown field %s; this rearm CLI sends only: %s", ref, quoteCut(k, maxRenderedKeyRunes), strings.Join(known, ", "))
		}
	}
	return nil
}

func isKnownKey(key string, known []string) bool {
	for _, n := range known {
		if strings.EqualFold(key, n) {
			return true
		}
	}
	return false
}

// deprecatedAliasNotices are the notices of design 3.4 item 3 for one artifact.
func deprecatedAliasNotices(ref string, a *Artifact) []string {
	var notices []string
	alias := func(oldName, oldValue, newName, newValue string) {
		if oldValue == "" {
			return
		}
		notices = append(notices, fmt.Sprintf("notice: %s: %q is deprecated; use %q", ref, oldName, newName))
		if newValue != "" && newValue != oldValue {
			notices = append(notices, fmt.Sprintf("notice: %s: %q (%s) is used and %q (%s) is ignored", ref, newName, renderToken(newValue), oldName, renderToken(oldValue)))
		}
	}
	alias("vexScope", a.VexScope, "importScope", a.ImportScope)
	alias("vexImportMode", a.VexImportMode, "importMode", a.ImportMode)
	return notices
}

// hasChecked: some entry's result is returned by the server, so a probe is needed.
func (in *addArtifactInput) hasChecked() bool {
	for _, e := range in.importable {
		if e.checked() {
			return true
		}
	}
	return false
}

// tagCheckedEntries appends to each checked entry, after its own tags, the check tag with a value of
// 16 random bytes in lowercase hex, distinct per entry (design 3.5 item 4).
func (in *addArtifactInput) tagCheckedEntries() error {
	seen := map[string]bool{}
	for _, e := range in.importable {
		if !e.checked() {
			continue
		}
		var v string
		for v == "" || seen[v] {
			b := make([]byte, 16)
			if _, err := io.ReadFull(importCheckRandom, b); err != nil {
				return fmt.Errorf("could not generate the import check tag: %v", err)
			}
			v = hex.EncodeToString(b)
		}
		seen[v] = true
		e.checkValue = v
		e.art.Tags = append(e.art.Tags, TagInput{Key: importCheckTagKey, Value: v})
	}
	return nil
}

// ---------------------------------------------------------------------------------------------
// Commands whose mutation runs no import (design 3.4a)

// refEntry is one decoded artifact of a command and its flag path.
type refEntry struct {
	ref string
	art Artifact
}

// collectRefEntries lists arts and their nested artifacts in decode order, as base[j] and
// base[j].artifacts[k].
func collectRefEntries(base string, arts []Artifact) []refEntry {
	var entries []refEntry
	for j, a := range arts {
		ref := fmt.Sprintf("%s[%d]", base, j)
		entries = append(entries, refEntry{ref: ref, art: a})
		entries = append(entries, collectRefEntries(ref+".artifacts", a.Artifacts)...)
	}
	return entries
}

// nonImportingEntriesCheck refuses the first VEX or SAFETY_CONTEXT entry, or with allow returns a
// notice for each.
func nonImportingEntriesCheck(command string, entries []refEntry, allow bool) ([]string, error) {
	var notices []string
	for _, e := range entries {
		if !isImportableType(e.art.Type) {
			continue
		}
		if !allow {
			return nil, fmt.Errorf("%s: %s stores a %s artifact but never imports it, so nothing would take effect; upload it with addartifact once the release's SBOM is indexed, or pass --%s to store it without an import", e.ref, command, renderToken(e.art.Type), allowImportFailureFlag)
		}
		notices = append(notices, fmt.Sprintf("notice: %s: %s stores this %s artifact but does not import it (accepted by --%s)", e.ref, command, renderToken(e.art.Type), allowImportFailureFlag))
	}
	return notices, nil
}

// refuseNonImportingEntries ends the command with exit 1 at the first VEX or SAFETY_CONTEXT entry,
// unless --allow-import-failure is on the command line, when it prints a notice for each and the
// command goes on as before.
func refuseNonImportingEntries(command string, entries []refEntry) {
	notices, err := nonImportingEntriesCheck(command, entries, allowImportFailure)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	for _, n := range notices {
		fmt.Fprintln(os.Stderr, n)
	}
}

// ---------------------------------------------------------------------------------------------
// Reading the answer (design 3.5)

type importAnswerDetail struct {
	Uuid              string          `json:"uuid"`
	Type              string          `json:"type"`
	DisplayIdentifier string          `json:"displayIdentifier"`
	Tags              []TagInput      `json:"tags"`
	ImportSummary     json.RawMessage `json:"importSummary"`
	VexImportSummary  json.RawMessage `json:"vexImportSummary"`
}

type importAnswerDeliverable struct {
	Uuid            string               `json:"uuid"`
	ArtifactDetails []importAnswerDetail `json:"artifactDetails"`
}

type importAnswer struct {
	ArtifactDetails           []importAnswerDetail      `json:"artifactDetails"`
	InboundDeliverableDetails []importAnswerDeliverable `json:"inboundDeliverableDetails"`
	VariantDetails            []struct {
		OutboundDeliverableDetails []importAnswerDeliverable `json:"outboundDeliverableDetails"`
	} `json:"variantDetails"`
}

// parseImportAnswer reads data.addArtifactProgrammatic; nil when it is absent or null.
func parseImportAnswer(data json.RawMessage) *importAnswer {
	var env struct {
		Result *importAnswer `json:"addArtifactProgrammatic"`
	}
	if len(data) == 0 || json.Unmarshal(data, &env) != nil {
		return nil
	}
	return env.Result
}

// findDeliverable is the first deliverable with this uuid (ignoring case), inbound first.
func (a *importAnswer) findDeliverable(uuid string) *importAnswerDeliverable {
	if uuid == "" {
		return nil
	}
	for i := range a.InboundDeliverableDetails {
		if strings.EqualFold(a.InboundDeliverableDetails[i].Uuid, uuid) {
			return &a.InboundDeliverableDetails[i]
		}
	}
	for v := range a.VariantDetails {
		out := a.VariantDetails[v].OutboundDeliverableDetails
		for i := range out {
			if strings.EqualFold(out[i].Uuid, uuid) {
				return &out[i]
			}
		}
	}
	return nil
}

// attribute is the one detail of the entry's target that carries the entry's check tag value and
// its type; false when the target is missing, no detail or several carry it, or it has another type.
func (a *importAnswer) attribute(e *importEntry) (*importAnswerDetail, bool) {
	if a == nil || e.checkValue == "" {
		return nil, false
	}
	var details []importAnswerDetail
	switch e.part {
	case partRelease:
		details = a.ArtifactDetails
	case partDeliverable:
		d := a.findDeliverable(e.deliverable)
		if d == nil {
			return nil, false
		}
		details = d.ArtifactDetails
	default:
		return nil, false
	}
	var match *importAnswerDetail
	n := 0
	for i := range details {
		for _, t := range details[i].Tags {
			if t.Key == importCheckTagKey && t.Value == e.checkValue {
				n++
				match = &details[i]
				break
			}
		}
	}
	if n != 1 || match.Type != e.artType {
		return nil, false
	}
	return match, true
}

// ---------------------------------------------------------------------------------------------
// Outcome (design 3.6)

type importDemotion struct {
	Reason string `json:"reason"`
	Count  *int64 `json:"count"`
}

// importSummaryRead is an import summary as the CLI selects it; a field the server does not serve
// or answers null is nil.
type importSummaryRead struct {
	StatementsTotal             *int64           `json:"statementsTotal"`
	ProposalsAutoAccepted       *int64           `json:"proposalsAutoAccepted"`
	ProposalsCreated            *int64           `json:"proposalsCreated"`
	StatementsUnchanged         *int64           `json:"statementsUnchanged"`
	StatementsUnmatched         *int64           `json:"statementsUnmatched"`
	StatementsErrored           *int64           `json:"statementsErrored"`
	ProposalsAutoRejected       *int64           `json:"proposalsAutoRejected"`
	AttestationsDeferred        *int64           `json:"attestationsDeferred"`
	StatementsAwaitingReconcile *int64           `json:"statementsAwaitingReconcile"`
	Demotions                   []importDemotion `json:"demotions"`
	OverrideOutcome             *string          `json:"overrideOutcome"`
	ErrorMessages               []*string        `json:"errorMessages"`
}

// summaryOf decodes the detail's summary field; nil when null, absent or unreadable.
func summaryOf(d *importAnswerDetail, field string) *importSummaryRead {
	raw := d.ImportSummary
	if field == rearm.VexImportSummaryField {
		raw = d.VexImportSummary
	}
	var s *importSummaryRead
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil {
		return nil
	}
	return s
}

// importState is what the CLI knows of an entry's result before reading counts.
type importState int

const (
	stateNested      importState = iota // nested entry uploaded with --allow-import-failure
	stateSce                            // top-level source code entry entry uploaded with --allow-import-failure
	stateUnsupported                    // the server reports no import result
	stateNotFound                       // the entry's artifact could not be identified in the answer
	stateAttributed                     // the entry's artifact was found; its summary decides
)

type importOutcome int

const (
	outcomeImported importOutcome = iota
	outcomeNotImported
	outcomeNotReportedSce
	outcomeNotReported
	outcomeNotFound
	outcomeNoResult
	outcomeNothingErrored
	outcomeNothingUnmatched
	outcomeNothingRead
)

func (o importOutcome) text() string {
	switch o {
	case outcomeImported:
		return "IMPORTED"
	case outcomeNotImported:
		return "NOT IMPORTED: a nested artifact runs no import"
	case outcomeNotReportedSce:
		return "NOT REPORTED: the server does not report import results for source code entry uploads"
	case outcomeNotReported:
		return "NOT REPORTED: this server does not report import results"
	case outcomeNotFound:
		return "NOT FOUND IN ANSWER: the artifact could not be identified in the server's answer"
	case outcomeNoResult:
		return "NO RESULT: the server stored the artifact but recorded no import result"
	case outcomeNothingErrored:
		return "NOTHING IMPORTED: statements errored"
	case outcomeNothingUnmatched:
		return "NOTHING IMPORTED: no statement matched a component in the release's SBOM"
	default:
		return "NOTHING IMPORTED: the server read no statement from the document"
	}
}

func count(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

// importedCount counts every statement the server stored: auto-accepted, auto-rejected, unchanged
// and staged; for SAFETY_CONTEXT the unmatched levels are inside proposalsCreated and are taken out.
func importedCount(artifactType string, s *importSummaryRead) int64 {
	n := count(s.ProposalsAutoAccepted) + count(s.ProposalsAutoRejected) + count(s.StatementsUnchanged) + count(s.ProposalsCreated)
	if artifactType == artifactTypeSafetyContext {
		n -= count(s.StatementsUnmatched)
	}
	if n < 0 {
		return 0
	}
	return n
}

// classifyImport decides an entry's outcome by the ordered table of design 3.6; only
// outcomeImported passes the gate.
func classifyImport(artifactType string, summary *importSummaryRead, state importState) importOutcome {
	switch state {
	case stateNested:
		return outcomeNotImported
	case stateSce:
		return outcomeNotReportedSce
	case stateUnsupported:
		return outcomeNotReported
	case stateNotFound:
		return outcomeNotFound
	}
	if summary == nil {
		return outcomeNoResult
	}
	if importedCount(artifactType, summary) > 0 {
		return outcomeImported
	}
	if count(summary.StatementsErrored) > 0 {
		return outcomeNothingErrored
	}
	if count(summary.StatementsUnmatched) > 0 {
		return outcomeNothingUnmatched
	}
	return outcomeNothingRead
}

// ---------------------------------------------------------------------------------------------
// Printed summary (design 3.6)

var (
	tokenPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	uuidPattern  = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)
)

// renderText prints server or user text on one line: its first 300 runes through strconv.Quote, so
// control and format characters appear as Go escapes, and a cut marker outside the quotes.
func renderText(v string) string {
	r := []rune(v)
	if len(r) > maxRenderedRunes {
		return strconv.Quote(string(r[:maxRenderedRunes])) + fmt.Sprintf(" (cut, %d characters)", len(r))
	}
	return strconv.Quote(v)
}

// quoteCut quotes the first n runes of v.
func quoteCut(v string, n int) string {
	if r := []rune(v); len(r) > n {
		v = string(r[:n])
	}
	return strconv.Quote(v)
}

// renderToken prints an enum value bare when it looks like one, else quoted as text.
func renderToken(v string) string {
	if tokenPattern.MatchString(v) {
		return v
	}
	return renderText(v)
}

func renderUuid(v string) string {
	if uuidPattern.MatchString(v) {
		return v
	}
	return renderText(v)
}

func renderCount(v *int64) string {
	if v == nil {
		return "-"
	}
	return strconv.FormatInt(*v, 10)
}

// importBlock is the printed block of one entry.
func importBlock(e *importEntry, outcome importOutcome, detail *importAnswerDetail, s *importSummaryRead) []string {
	header := fmt.Sprintf("import %s %s %s", renderToken(e.artType), e.ref, renderText(e.displayId))
	if detail != nil {
		header += " artifact " + renderUuid(detail.Uuid)
	}
	lines := []string{header + ": " + outcome.text()}
	if s == nil {
		return lines
	}
	lines = append(lines, fmt.Sprintf("  total=%s auto-accepted=%s staged=%s unchanged=%s unmatched=%s errored=%s auto-rejected=%s attestations-deferred=%s awaiting-sbom=%s",
		renderCount(s.StatementsTotal), renderCount(s.ProposalsAutoAccepted), renderCount(s.ProposalsCreated),
		renderCount(s.StatementsUnchanged), renderCount(s.StatementsUnmatched), renderCount(s.StatementsErrored),
		renderCount(s.ProposalsAutoRejected), renderCount(s.AttestationsDeferred), renderCount(s.StatementsAwaitingReconcile)))
	if len(s.Demotions) > 0 {
		reasons := make([]string, 0, len(s.Demotions))
		for _, d := range s.Demotions {
			reasons = append(reasons, renderToken(d.Reason)+"="+renderCount(d.Count))
		}
		lines = append(lines, "  staged-by-reason: "+strings.Join(reasons, " "))
	}
	if s.OverrideOutcome != nil && *s.OverrideOutcome != overrideOutcomeNone {
		lines = append(lines, "  issuer-override: "+renderToken(*s.OverrideOutcome))
	}
	for _, d := range s.Demotions {
		if d.Reason == noFindingAnalysisWriteReason && count(d.Count) > 0 {
			lines = append(lines, "  note: the uploading key does not hold Finding Analysis write at the import scope; these statements wait for review")
			break
		}
	}
	for i, m := range s.ErrorMessages {
		if i == maxPrintedMessages {
			lines = append(lines, fmt.Sprintf("  (+%d more messages)", len(s.ErrorMessages)-maxPrintedMessages))
			break
		}
		text := ""
		if m != nil {
			text = *m
		}
		lines = append(lines, "  message: "+renderText(text))
	}
	return lines
}

// finalImportLine is the last line of the summary: k importable entries, f of them not imported.
func finalImportLine(k, f int, allowFailure bool) string {
	switch {
	case f == 0:
		return fmt.Sprintf("import check passed: %d of %d VEX/SAFETY_CONTEXT artifacts imported", k, k)
	case allowFailure:
		return fmt.Sprintf("import check: %d of %d VEX/SAFETY_CONTEXT artifacts imported nothing or have no known result; accepted by --%s", f, k, allowImportFailureFlag)
	default:
		return fmt.Sprintf("import check failed: %d of %d VEX/SAFETY_CONTEXT artifacts imported nothing or have no known result; the artifacts are stored (exit 3). Pass --%s to accept this.", f, k, allowImportFailureFlag)
	}
}

// importSummaryLines classifies every importable entry against the answer and returns the printed
// lines and the number of entries not imported.
func (in *addArtifactInput) importSummaryLines(data json.RawMessage, support rearm.ImportSupport) ([]string, int) {
	answer := parseImportAnswer(data)
	var lines []string
	failed := 0
	for _, e := range in.importable {
		var (
			state  importState
			detail *importAnswerDetail
			s      *importSummaryRead
		)
		switch {
		case e.nested:
			state = stateNested
		case e.part == partSce:
			state = stateSce
		case support.Field == "":
			state = stateUnsupported
		default:
			if d, ok := answer.attribute(e); ok {
				state, detail = stateAttributed, d
				s = summaryOf(d, support.Field)
			} else {
				state = stateNotFound
			}
		}
		outcome := classifyImport(e.artType, s, state)
		lines = append(lines, importBlock(e, outcome, detail, s)...)
		if outcome != outcomeImported {
			failed++
		}
	}
	lines = append(lines, finalImportLine(len(in.importable), failed, in.allowFailure))
	return lines, failed
}

// ---------------------------------------------------------------------------------------------
// Upload, retry and exit (design 3.5 items 3, 5, 6; 3.7; 3.8)

// resultPresent: data.addArtifactProgrammatic is present and not null.
func resultPresent(data json.RawMessage) bool {
	var env map[string]json.RawMessage
	if len(data) == 0 || json.Unmarshal(data, &env) != nil {
		return false
	}
	r, ok := env["addArtifactProgrammatic"]
	return ok && len(r) > 0 && string(r) != "null"
}

// isSbomPrecondition: the server refused the call because the release's SBOM is not indexed yet
// (every error carries the marker and there is no result), so nothing was stored.
func isSbomPrecondition(data json.RawMessage, err error) bool {
	var gqlErrs rearm.GraphQLErrors
	if !errors.As(err, &gqlErrs) || len(gqlErrs) == 0 || resultPresent(data) {
		return false
	}
	for _, e := range gqlErrs {
		if !strings.Contains(e.Message, sbomNotIndexedMarker) {
			return false
		}
	}
	return true
}

func graphQLMessages(err error) string {
	var gqlErrs rearm.GraphQLErrors
	if !errors.As(err, &gqlErrs) {
		return err.Error()
	}
	messages := make([]string, 0, len(gqlErrs))
	for _, e := range gqlErrs {
		messages = append(messages, e.Message)
	}
	return strings.Join(messages, "; ")
}

// formatSeconds prints a wait in seconds, to the millisecond.
func formatSeconds(d time.Duration) string {
	return strconv.FormatFloat(d.Truncate(time.Millisecond).Seconds(), 'f', -1, 64)
}

// uploadWaitingForSbom sends the upload, and with --wait-for-sbom sends the identical request again
// while the server answers that the release's SBOM is not indexed, until the duration is used up
// (timedOut). Each attempt builds new file parts over the bytes held in memory.
func uploadWaitingForSbom(query string, variables map[string]interface{}, locationMap map[string][]string, filesMap map[string]interface{}) (data json.RawMessage, err error, timedOut bool) {
	start := importNow()
	for attempt := 1; ; attempt++ {
		data, err = uploadMultipartResult(query, variables, locationMap, filesMap)
		if waitForSbom <= 0 || !isSbomPrecondition(data, err) {
			return data, err, false
		}
		elapsed := importNow().Sub(start)
		if elapsed >= waitForSbom {
			return data, err, true
		}
		wait := sbomWaitBackoff[min(attempt-1, len(sbomWaitBackoff)-1)]
		if remaining := waitForSbom - elapsed; wait > remaining {
			wait = remaining
		}
		fmt.Fprintf(os.Stderr, "waiting for the release's SBOM to be indexed; retry %d in %ss\n", attempt, formatSeconds(wait))
		importSleep(wait)
	}
}

// runAddArtifactImport uploads an input that has importable entries and prints the summary; it
// returns the exit code (design 3.7).
func runAddArtifactImport(in *addArtifactInput, variables map[string]interface{}, locationMap map[string][]string, filesMap map[string]interface{}) int {
	query := rearm.AddArtifactProgrammatic_Operation
	var support rearm.ImportSupport
	if in.hasChecked() {
		s, err := rearm.ProbeImportSummary(context.Background(), rearmClient())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: could not read which import result fields this server reports: %s; nothing was uploaded\n", describeError(err))
			return 1
		}
		support = s
		if s.Field != "" {
			query = rearm.AddArtifactWithImportsOperation(rearm.ImportSelection{Support: s, Deliverables: in.hasDeliverable})
		}
	}
	if debug == "true" {
		jsonBody, _ := json.Marshal(map[string]interface{}{"query": query, "variables": variables})
		fmt.Println("GraphQL Request:")
		fmt.Println(string(jsonBody))
	}

	data, err, timedOut := uploadWaitingForSbom(query, variables, locationMap, filesMap)
	if err != nil {
		var gqlErrs rearm.GraphQLErrors
		if errors.As(err, &gqlErrs) && resultPresent(data) {
			// the upload is stored, the answer carries errors too (design 3.5 item 5)
			printUploadEnvelope(data)
			for _, e := range gqlErrs {
				fmt.Fprintln(os.Stderr, "warning: the server answered with an error after storing the upload: "+renderText(e.Message))
			}
			return in.printImportSummary(data, support)
		}
		printUploadFailure(query, data, err)
		switch {
		case timedOut:
			fmt.Fprintf(os.Stderr, "Error: the release's SBOM was not indexed within %s; nothing was uploaded. Server: %s\n", waitForSbom, renderText(graphQLMessages(err)))
		case isSbomPrecondition(data, err):
			fmt.Fprintf(os.Stderr, "hint: upload the SBOM first; pass --%s 10m to retry until it is indexed\n", waitForSbomFlag)
		case errors.As(err, &gqlErrs):
			fmt.Fprintln(os.Stderr, "no import result is reported: the server refused the call; parts it processed before the refusal may be stored")
		default:
			fmt.Fprintln(os.Stderr, "the upload may or may not have been stored; no import result is known")
		}
		return 1
	}
	printUploadEnvelope(data)
	return in.printImportSummary(data, support)
}

// printImportSummary prints the blocks and the final line on stderr and returns the gate's exit code.
func (in *addArtifactInput) printImportSummary(data json.RawMessage, support rearm.ImportSupport) int {
	lines, failed := in.importSummaryLines(data, support)
	for _, l := range lines {
		fmt.Fprintln(os.Stderr, l)
	}
	if failed > 0 && !in.allowFailure {
		return 3
	}
	return 0
}
