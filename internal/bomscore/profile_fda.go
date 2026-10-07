package bomscore

const noteVulnerabilities = "delivered as VDR/VEX with the submission, not in the SBOM"

// profileFDA: FDA premarket cybersecurity guidance (issued 2026-02-03), section V.A.4(b): the eight
// baseline attributes of the NTIA Framing document, 2nd edition (October 2021), section 2.2, plus
// the level of support and the end-of-support date of each component.
var profileFDA = Profile{
	Key:   ProfileFDA,
	Title: "FDA Premarket SBOM (2026)",
	Source: Source{
		Title:   `FDA, "Cybersecurity in Medical Devices: Quality Management System Considerations and Content of Premarket Submissions", section V.A.4(b)`,
		Version: "",
		Date:    "2026-02-03",
		URL:     "https://www.fda.gov/media/119933/download",
	},
	Checks: []Check{
		fdaBaseline("author-name", "Author Name", ScopeDocument, "", hasSBOMAuthor),
		fdaBaseline("timestamp", "Timestamp", ScopeDocument, "", hasTimestamp),
		fdaBaseline("supplier-name", "Supplier Name", ScopeComponent, remedyEnrich, hasComponentProducer),
		fdaBaseline("component-name", "Component Name", ScopeComponent, "", hasComponentName),
		fdaBaseline("version-string", "Version String", ScopeComponent, "", hasComponentVersion),
		// Operator point O1: the Framing document's eighth attribute. Delete this line (and its
		// test row) to score the seven July 2021 fields only.
		fdaBaseline("component-hash", "Component Hash", ScopeComponent, "", hasComponentHash),
		fdaBaseline("unique-identifier", "Unique Identifier", ScopeComponent, "", hasComponentIdentifier),
		fdaBaseline("relationship", "Relationship", ScopeDocument, remedyDependencies, declaresDirectDependencies),
		{ID: string(ProfileFDA) + ".component.support-level", Title: "Software level of support", Level: LevelRequired,
			Scope: ScopeComponent, Ref: "V.A.4(b)", Remedy: remedySupport, Eval: hasSupportLevel},
		{ID: string(ProfileFDA) + ".component.end-of-support", Title: "End-of-support date", Level: LevelRequired,
			Scope: ScopeComponent, Ref: "V.A.4(b)", Remedy: remedySupport, Eval: hasEndOfSupport},
		{ID: string(ProfileFDA) + ".document.vulnerabilities", Title: "Known vulnerabilities and their assessment", Level: LevelInfo,
			Scope: ScopeDocument, Ref: "V.A.4(b)", Eval: notAssessed(noteVulnerabilities)},
	},
}

func fdaBaseline(slug, title string, scope Scope, remedy string, eval func(*Doc) Outcome) Check {
	return Check{ID: string(ProfileFDA) + ".baseline." + slug, Title: title, Level: LevelRequired, Scope: scope,
		Ref: "V.A.4(b); NTIA Framing 2nd ed. 2.2, " + title, Remedy: remedy, Eval: eval}
}
