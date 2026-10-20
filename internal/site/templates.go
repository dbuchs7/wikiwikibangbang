package site

// Templates are regulations an installation may adopt and then adapt
// (ADR-0058). Their categories follow the law; their indicators are a
// starting point, words that often accompany such data in German and
// English, to be tuned by the organisation. They are neither complete nor
// a legal assessment.
var Templates = []Regulation{
	{
		ID:   "dsg",
		Name: "DSG – Bundesgesetz über den Datenschutz (SR 235.1), Art. 5 Bst. c",
		URL:  "https://www.fedlex.admin.ch/eli/cc/2022/491/de",
		Categories: []Category{
			{Name: "Religiöse, weltanschauliche, politische oder gewerkschaftliche Ansichten oder Tätigkeiten",
				Indicators: []string{"Religion", "Konfession", "Kirchenaustritt", "Weltanschauung", "Parteimitglied", "Parteizugehörigkeit", "politische Gesinnung", "Gewerkschaft", "religion", "trade union"}},
			{Name: "Gesundheit, Intimsphäre",
				Indicators: []string{"Diagnose", "Arztzeugnis", "Arztbericht", "Krankschreibung", "Arbeitsunfähigkeit", "Krankheit", "Therapie", "Medikament", "Behinderung", "IV-Rente", "Schwangerschaft", "psychiatrisch", "Sucht", "diagnosis", "medical certificate", "sick leave"}},
			{Name: "Zugehörigkeit zu einer Rasse oder Ethnie",
				Indicators: []string{"Ethnie", "ethnische Herkunft", "Hautfarbe", "ethnic origin"}},
			{Name: "Genetische Daten",
				Indicators: []string{"Gentest", "genetisch", "DNA-Analyse", "genetic"}},
			{Name: "Biometrische Daten, die eine natürliche Person eindeutig identifizieren",
				Indicators: []string{"Fingerabdruck", "Gesichtserkennung", "biometrisch", "Iris-Scan", "fingerprint", "biometric"}},
			{Name: "Verwaltungs- und strafrechtliche Verfolgungen oder Sanktionen",
				Indicators: []string{"Strafregister", "Vorstrafe", "Strafverfahren", "Strafbefehl", "Verurteilung", "Disziplinarverfahren", "criminal record", "conviction"}},
			{Name: "Massnahmen der sozialen Hilfe",
				Indicators: []string{"Sozialhilfe", "Fürsorge", "KESB", "Beistandschaft", "Ergänzungsleistung", "social assistance"}},
		},
	},
	{
		ID:   "dsgvo",
		Name: "DSGVO – Verordnung (EU) 2016/679, Art. 9 und 10",
		URL:  "https://eur-lex.europa.eu/eli/reg/2016/679/oj",
		Categories: []Category{
			{Name: "Rassische und ethnische Herkunft (Art. 9)",
				Indicators: []string{"Ethnie", "ethnische Herkunft", "Hautfarbe", "ethnic origin", "racial"}},
			{Name: "Politische Meinungen (Art. 9)",
				Indicators: []string{"Parteimitglied", "Parteizugehörigkeit", "politische Gesinnung", "political opinion", "party member"}},
			{Name: "Religiöse oder weltanschauliche Überzeugungen (Art. 9)",
				Indicators: []string{"Religion", "Konfession", "Kirchenaustritt", "Weltanschauung", "religion", "religious belief"}},
			{Name: "Gewerkschaftszugehörigkeit (Art. 9)",
				Indicators: []string{"Gewerkschaft", "trade union"}},
			{Name: "Genetische Daten (Art. 9)",
				Indicators: []string{"Gentest", "genetisch", "DNA-Analyse", "genetic"}},
			{Name: "Biometrische Daten zur eindeutigen Identifizierung (Art. 9)",
				Indicators: []string{"Fingerabdruck", "Gesichtserkennung", "biometrisch", "fingerprint", "biometric"}},
			{Name: "Gesundheitsdaten (Art. 9)",
				Indicators: []string{"Diagnose", "Arztzeugnis", "Krankschreibung", "Arbeitsunfähigkeit", "Krankheit", "Therapie", "Medikament", "Behinderung", "Schwangerschaft", "diagnosis", "medical certificate", "sick leave", "disability"}},
			{Name: "Sexualleben oder sexuelle Orientierung (Art. 9)",
				Indicators: []string{"sexuelle Orientierung", "homosexuell", "bisexuell", "transgender", "sexual orientation"}},
			{Name: "Strafrechtliche Verurteilungen und Straftaten (Art. 10)",
				Indicators: []string{"Strafregister", "Vorstrafe", "Strafverfahren", "Verurteilung", "criminal record", "conviction"}},
		},
	},
}
