package bomscore

const noteVulnerabilities = "delivered as VDR/VEX with the submission, not in the SBOM"

// FDARuleSetVersion names the published rule list of the fda profile, docs/fda-checker-rules.md.
// Raise it by one in the change that alters that list in any way (TestFDARuleSetPinned), and in a
// change that makes an fda check decide differently on some document while the list reads the same.
const FDARuleSetVersion = "1"

// profileFDA: FDA premarket cybersecurity guidance (issued 2026-02-03), section V.A.4(b): the eight
// baseline attributes of the NTIA Framing document, 2nd edition (October 2021), section 2.2, plus
// the level of support and the end-of-support date of each component.
var profileFDA = Profile{
	Key:            ProfileFDA,
	Title:          "FDA Premarket SBOM (2026)",
	RuleSetVersion: FDARuleSetVersion,
	Source: Source{
		Title:   `FDA, "Cybersecurity in Medical Devices: Quality Management System Considerations and Content of Premarket Submissions", section V.A.4(b)`,
		Version: "",
		Date:    "2026-02-03",
		URL:     "https://www.fda.gov/media/119933/download",
	},
	Checks: []Check{
		fdaBaseline("author-name", "Author Name", ScopeDocument, "", hasSBOMAuthor,
			"The document names an author: CycloneDX `metadata.authors[].name` or `metadata.manufacturer.name`; SPDX a `Person:` or `Organization:` creator. A tool, `metadata.supplier` and the deprecated `metadata.manufacture` are not an author."),
		fdaBaseline("timestamp", "Timestamp", ScopeDocument, "", hasTimestamp,
			"The document has a creation timestamp, CycloneDX `metadata.timestamp` or SPDX `created`, in one of the timestamp forms below."),
		fdaBaseline("supplier-name", "Supplier Name", ScopeComponent, remedyEnrich, hasComponentProducer,
			"Every component names who made or supplies it: CycloneDX `manufacturer.name`, `supplier.name`, `authors[].name` or `author`; SPDX `supplier` or `originator`."),
		fdaBaseline("component-name", "Component Name", ScopeComponent, "", hasComponentName,
			"Every component has a name."),
		fdaBaseline("version-string", "Version String", ScopeComponent, "", hasComponentVersion,
			"Every component has a version."),
		// Operator point O1: the Framing document's eighth attribute. Delete this line (and its
		// test row) to score the seven July 2021 fields only.
		fdaBaseline("component-hash", "Component Hash", ScopeComponent, "", hasComponentHash,
			"Every component has a hash entry with both an algorithm and a value (CycloneDX `hashes[]`; SPDX `checksums[]`), or, in CycloneDX, a property `reliza:support:justifiedUnknown:hash` whose value is one of the reason codes below."),
		fdaBaseline("unique-identifier", "Unique Identifier", ScopeComponent, "", hasComponentIdentifier,
			"Every component has an identifier: CycloneDX `purl`, `cpe`, `swid.tagId`, `swhid[]` or `omniborId[]`; SPDX an external reference of type `purl`, `cpe22Type`, `cpe23Type`, `swh` or `gitoid`."),
		fdaBaseline("relationship", "Relationship", ScopeDocument, remedyDependencies, declaresDirectDependencies,
			"Every described component declares its direct dependencies or states that it has none; what another component declares does not count. CycloneDX: a `dependencies[]` entry whose `ref` is the `bom-ref` of `metadata.component`, with a non-empty `dependsOn`; or a `compositions[]` entry with `aggregate` `complete` whose `dependencies[]` lists that `bom-ref`. SPDX, for each described package: `DEPENDS_ON` or `CONTAINS` from it to a package of the document or to `NONE`; or the mirrors, `DEPENDENCY_OF` or `CONTAINED_BY` from a package of the document or from `NONE` to it. Any other end (a file, `NOASSERTION`, another document, an unknown id) does not count. The check fails when nothing is described: CycloneDX without `metadata.component` or without its `bom-ref`; SPDX without a `DESCRIBES` relationship from the document to one of its packages, or `DESCRIBED_BY` the other way."),
		{ID: string(ProfileFDA) + ".component.support-level", Title: "Software level of support", Level: LevelRequired,
			Scope: ScopeComponent, Ref: "V.A.4(b)", Remedy: remedySupport, Eval: hasSupportLevel,
			What: "Every judged component has a property `reliza:support:levelOfSupport` whose value is one of the level-of-support values below; one such property is enough, whatever another of that name says. Which components are judged is said below. SPDX 2.x has no place for the level of support: every package fails and none is skipped."},
		{ID: string(ProfileFDA) + ".component.end-of-support", Title: "End-of-support date", Level: LevelRequired,
			Scope: ScopeComponent, Ref: "V.A.4(b)", Remedy: remedySupport, Eval: hasEndOfSupport,
			What: "Every judged component has an end-of-support date: CycloneDX a property `cdx:lifecycle:milestone:endOfSupport`, SPDX 2.3 `validUntilDate`, whose value is in one of the end-of-support date forms below; or, in CycloneDX, a property `reliza:support:justifiedUnknown:endOfSupport` whose value is one of the reason codes below. One such property is enough, whatever another of that name says. Which components are judged is said below. SPDX 2.1 and 2.2 have no place for the date: every package fails and none is skipped."},
		{ID: string(ProfileFDA) + ".document.vulnerabilities", Title: "Known vulnerabilities and their assessment", Level: LevelInfo,
			Scope: ScopeDocument, Ref: "V.A.4(b)", Eval: notAssessed(noteVulnerabilities),
			What: "Nothing is read: known vulnerabilities and their assessment are " + noteVulnerabilities + ". Always NOT_ASSESSED."},
	},
}

func fdaBaseline(slug, title string, scope Scope, remedy string, eval func(*Doc) Outcome, what string) Check {
	return Check{ID: string(ProfileFDA) + ".baseline." + slug, Title: title, Level: LevelRequired, Scope: scope,
		Ref: "V.A.4(b); NTIA Framing 2nd ed. 2.2, " + title, Remedy: remedy, What: what, Eval: eval}
}
