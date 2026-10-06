package bomscore

const notePractice = "a practice of the SBOM author; one file cannot show it"

// profileCISA2026: CISA et al., 2026 Minimum Elements for an SBOM, version 2.1, Table 1 (17 data
// fields), then the six practices as INFO checks.
var profileCISA2026 = Profile{
	Key:   ProfileCISA2026,
	Title: "CISA Minimum Elements for an SBOM (2026)",
	Source: Source{
		Title:   `CISA et al., "2026 Minimum Elements for a Software Bill of Materials (SBOM)"`,
		Version: "2.1",
		Date:    "2026-07-29",
		URL:     "https://www.cisa.gov/sites/default/files/2026-07/2026_cisa_sbom_minimum_elements_508c.pdf",
	},
	Checks: []Check{
		cisaField("sbom-author", "SBOM Author", ScopeDocument, "", hasSBOMAuthor),
		cisaField("sbom-author-signature", "SBOM Author Signature", ScopeDocument, "", hasSignature),
		cisaField("sbom-data-format-name", "SBOM Data Format Name", ScopeDocument, "", hasFormatName),
		cisaField("sbom-data-format-version", "SBOM Data Format Version", ScopeDocument, "", hasFormatVersion),
		cisaField("sbom-generation-context", "SBOM Generation Context", ScopeDocument, "", hasGenerationContext),
		cisaField("sbom-timestamp", "SBOM Timestamp", ScopeDocument, "", hasTimestamp),
		cisaField("sbom-tool-name", "SBOM Tool Name", ScopeDocument, "", hasToolName),
		cisaField("sbom-tool-version", "SBOM Tool Version", ScopeDocument, "", hasToolVersion),
		cisaField("sbom-version", "SBOM Version", ScopeDocument, "", hasSBOMVersion),
		cisaField("component-name", "Component Name", ScopeComponent, "", hasComponentName),
		cisaField("component-version", "Component Version", ScopeComponent, "", hasComponentVersion),
		cisaField("component-producer", "Component Producer", ScopeComponent, remedyEnrich, hasComponentProducer),
		cisaField("component-identifiers", "Component Identifiers", ScopeComponent, "", hasComponentIdentifier),
		cisaField("component-hash-algorithm", "Component Hash Algorithm", ScopeComponent, "", hasComponentHashAlgorithm),
		cisaField("component-hash-value", "Component Hash Value", ScopeComponent, "", hasComponentHashValue),
		cisaField("component-license", "Component License", ScopeComponent, remedyEnrich, hasComponentLicense),
		cisaField("component-dependency-relationship", "Component Dependency Relationship", ScopeDocument, "", hasDependencyRelationship),
		cisaPractice("updates", "Accommodation of Updates to SBOM Data"),
		cisaPractice("coverage", "Coverage"),
		cisaPractice("distribution", "Distribution and Delivery"),
		cisaPractice("unknowns", "Explicitly Identifying Unknown Information"),
		cisaPractice("frequency", "Frequency"),
		cisaPractice("machine-processable", "Machine-Processable Data"),
	},
}

func cisaField(slug, title string, scope Scope, remedy string, eval func(*Doc) Outcome) Check {
	return Check{ID: string(ProfileCISA2026) + "." + slug, Title: title, Level: LevelRequired, Scope: scope,
		Ref: "Table 1, " + title, Remedy: remedy, Eval: eval}
}

func cisaPractice(slug, heading string) Check {
	return Check{ID: string(ProfileCISA2026) + ".practice." + slug, Title: heading, Level: LevelInfo, Scope: ScopeDocument,
		Ref: heading, Eval: notAssessed(notePractice)}
}
