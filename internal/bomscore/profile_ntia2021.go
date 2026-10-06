package bomscore

// profileNTIA2021: NTIA, The Minimum Elements For a Software Bill of Materials (SBOM), 2021-07-12,
// the seven fields of the Data Fields table.
var profileNTIA2021 = Profile{
	Key:   ProfileNTIA2021,
	Title: "NTIA Minimum Elements for an SBOM (2021)",
	Source: Source{
		Title:   `NTIA, "The Minimum Elements For a Software Bill of Materials (SBOM)"`,
		Version: "",
		Date:    "2021-07-12",
		URL:     "https://www.ntia.gov/files/ntia/publications/sbom_minimum_elements_report.pdf",
	},
	Checks: []Check{
		ntiaField("supplier-name", "Supplier Name", ScopeComponent, remedyEnrich, hasComponentProducer),
		ntiaField("component-name", "Component Name", ScopeComponent, "", hasComponentName),
		ntiaField("component-version", "Version of the Component", ScopeComponent, "", hasComponentVersion),
		ntiaField("other-unique-identifiers", "Other Unique Identifiers", ScopeComponent, "", hasComponentIdentifier),
		ntiaField("dependency-relationship", "Dependency Relationship", ScopeDocument, "", hasDependencyRelationship),
		ntiaField("author-of-sbom-data", "Author of SBOM Data", ScopeDocument, "", hasSBOMAuthor),
		ntiaField("timestamp", "Timestamp", ScopeDocument, "", hasTimestamp),
	},
}

func ntiaField(slug, title string, scope Scope, remedy string, eval func(*Doc) Outcome) Check {
	return Check{ID: string(ProfileNTIA2021) + "." + slug, Title: title, Level: LevelRequired, Scope: scope,
		Ref: "Data Fields, " + title, Remedy: remedy, Eval: eval}
}
