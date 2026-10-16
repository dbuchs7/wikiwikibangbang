# ADR-0001: Gemeinsames Item-Datenmodell für Wissen und Arbeit

- **Status:** Proposed
- **Implementation:** Not started
- **Date:** 2026-10-16
- **Deciders:** Projektinitianten
- **Open question:** Ist ein Space selbst ein Item oder ein eigenständiger Container? Wie werden Typ-Schemas migriert, wenn bereits Instanzen existieren? (siehe Abschnitt «Offene Fragen»)
- **Question checked:** 2026-10

## Kontext und Problemstellung

wikiwikibangbang soll die Funktionen von Confluence (Wissen: Seiten, Seitenbaum, Kommentare, Versionen) und Jira (Arbeit: Issues, Felder, Workflows, Boards, Abfragen) in **einer** Lösung vereinen.

Zerlegt man beide Produkte in ihre Grundbestandteile, ergibt sich folgendes Bild:

| Bestandteil | Confluence-Seite | Jira-Issue |
|---|---|---|
| Stabile Identität und menschenlesbarer Schlüssel | ja (Titel/ID) | ja (`PROJ-123`) |
| Container | Space | Projekt |
| Hierarchie | Seitenbaum | Epic → Story → Sub-Task |
| Freitext-Inhalt | Hauptsache (Rich Document) | Nebensache (Beschreibung) |
| Strukturierte Eigenschaften | wenige (Labels, Owner) | Hauptsache (Felder) |
| Lebenszyklus | selten (Entwurf/Publiziert) | Hauptsache (Workflow) |
| Beziehungen zu anderen Objekten | Links, Erwähnungen | Issue-Links |
| Kommentare, Anhänge, Beobachter, Historie, Berechtigungen | ja | ja |

Die beiden Objektarten unterscheiden sich nicht in ihrer **Art**, sondern in der **Gewichtung** derselben Bestandteile. Atlassian modelliert sie dennoch als zwei getrennte Systeme mit Verlinkung dazwischen. Daraus folgen doppelte Implementationen von Suche, Berechtigungen, Historie und Kommentaren sowie eine Integration, die an der Systemgrenze endet: Eine Checkliste in einer Seite wird nicht zum Issue, ein Issue kennt keinen Workflow für seine Dokumentation, eine Entscheidung (ADR) hat keinen Platz in beiden.

Die Frage lautet: **Welches Datenmodell liegt allen nutzergenerierten Inhalten zugrunde?** Diese Entscheidung bestimmt Suche, Berechtigung, Historie, Import, API und UI und ist nachträglich nur mit sehr hohem Aufwand änderbar.

## Entscheidungstreiber

- **Echte Vereinigung:** Querschnittsfunktionen (Suche, Berechtigung, Historie, Kommentare, Beziehungen) sollen genau einmal existieren.
- **Strukturierte Abfragbarkeit** auf Jira-Niveau (Felder, Zustände, Aggregation für Boards und Berichte).
- **Kollaboratives Bearbeiten** von Dokumenten auf Confluence-Niveau, ohne die strukturierten Daten zu gefährden.
- **Determinismus und Nachvollziehbarkeit:** Jede Änderung ist ein benennbares Ereignis mit Urheber und Zeitpunkt.
- **Verständlichkeit für Nutzer:** Das Modell darf in der UI nicht als «alles ist ein generischer Block» durchschlagen.
- **Migrierbarkeit** aus Confluence und Jira ohne Informationsverlust bei den Kernobjekten.

## Betrachtete Optionen

1. **Zwei Domänenmodelle** (`Page`, `Issue`) mit Verlinkung (Atlassian-Ansatz)
2. **Alles ist ein Block** (Notion-Ansatz): Seiten, Absätze und Datenbankzeilen sind Blöcke in einem Baum
3. **Generischer Graph** (Tripel-/Property-Graph ohne festes Schema)
4. **Ein typisiertes Item-Modell:** Jedes Objekt ist ein *Item* mit Typ, Eigenschaften, optionalem Dokumentkörper, optionalem Lebenszyklus und typisierten Beziehungen

## Entscheidung

Gewählt wird **Option 4: ein typisiertes Item-Modell.**

### Aufbau eines Items

```text
Item
├── id            unveränderliche ID (ULID), global eindeutig
├── key           menschenlesbarer Alias je Space (z. B. "PLAT-42", "betriebshandbuch")
├── type          Verweis auf eine versionierte Typdefinition (page, task, bug, epic, decision, …)
├── space         genau ein Space (Container, Berechtigungsgrenze)
├── parent        höchstens ein Eltern-Item (Containment-Baum)
├── properties    typisierte Feldwerte, gegen das Schema des Typs validiert
├── state         aktueller Lebenszykluszustand (nur wenn der Typ einen Workflow hat)
├── body          optionales strukturiertes Dokument (Block-Dokument, kein HTML/Markdown als Speicherformat)
├── relations     typisierte, gerichtete Kanten zu anderen Items (eigene Datensätze)
└── universal     Kommentare, Anhänge, Beobachter, Labels, Historie, Berechtigungen
```

### Kernentscheidungen innerhalb des Modells

1. **Typen sind Daten, nicht Code.** Eine Typdefinition legt Felder, Pflichtfelder, Workflow und Darstellung fest. Das System liefert eingebaute Typen (`page`, `task`, `bug`, `epic`, `decision`) mit festen Voreinstellungen. Eigene Typen werden auf demselben Mechanismus aufgebaut. Ein Item ist darum nicht «Seite oder Issue», sondern trägt einen Typ, der festlegt, wie stark Dokument, Felder und Workflow gewichtet sind.

2. **Zwei Änderungsgeschwindigkeiten.**
   - *Eigenschaften, Zustand, Beziehungen* ändern sich diskret: Jede Änderung ist ein validiertes, einzelnes Ereignis.
   - *Der Dokumentkörper* ändert sich kontinuierlich (kollaboratives Tippen). Feingranulare Änderungen werden in einer eigenen Kollaborationsschicht zusammengeführt und nur in groben Abständen als **Version** in die autoritative Historie übernommen.

   Die beiden Wege werden nie vermischt. Damit kann eine Tippsitzung keinen Feldwert verändern, und die Ereignishistorie wird nicht pro Tastendruck aufgebläht.

3. **Strukturierte Daten leben in Eigenschaften, nicht im Dokument.** Was abgefragt, gefiltert oder aggregiert werden muss, ist eine Eigenschaft. Der Dokumentkörper darf Items *referenzieren* (eingebettetes Issue, Live-Abfrage, Erwähnung), aber nicht deren autoritative Daten enthalten. Erwähnungen im Dokument erzeugen **abgeleitete Beziehungen**, die bei jeder neuen Version deterministisch neu berechnet werden.

4. **Genau ein Containment-Baum.** `parent` bestimmt Navigation und die Vererbung von Berechtigungen. Alle anderen Hierarchien (Epic → Story, «dokumentiert», «blockiert», «implementiert») sind typisierte Beziehungen. Damit kann eine Story in einer Seite angelegt werden und dennoch zu einem Epic in einem anderen Bereich gehören, ohne zwei konkurrierende Bäume.

5. **Beziehungen sind eigene Datensätze** mit Typ, Richtung, Urheber und Historie, indiziert von beiden Enden. Beziehungen über Space-Grenzen hinweg sind erlaubt. Ist das Ziel für den Betrachter nicht sichtbar, wird nur «eingeschränktes Item» angezeigt, ohne Titel oder Schlüssel.

6. **Zustand ändert sich nur über Transitionen.** Hat ein Typ einen Workflow, ist `state` keine frei schreibbare Eigenschaft. Transitionen können Bedingungen und Pflichtfelder haben. Seiten können damit ebenfalls Workflows haben (Entwurf → Review → Publiziert), ohne Sonderlogik.

7. **Querschnittsfunktionen existieren einmal.** Kommentare, Anhänge, Beobachter, Historie, Berechtigungsprüfung, Volltext- und Feldsuche sowie Benachrichtigungen werden für `Item` implementiert, nie für einen einzelnen Typ.

8. **ID und Schlüssel sind getrennt.** Die `id` ändert sich nie. Der `key` ist ein Alias und darf sich beim Verschieben zwischen Spaces ändern. Alte Schlüssel bleiben als Weiterleitung erhalten.

### Abgeleitete Invarianten

Diese Regeln werden in `docs/architecture/invariants.md` übernommen, sobald das ADR akzeptiert ist:

- **D1 – Ein Modell für Inhalte:** Nutzergenerierte Inhalte sind ausschliesslich Items. Es entsteht kein paralleles Domänenobjekt für einen einzelnen Inhaltstyp. Systemobjekte (Benutzer, Gruppen, Spaces, Typdefinitionen) sind davon ausgenommen.
- **D2 – Stabile Identität:** Verweise im System (Beziehungen, Dokumentreferenzen, Ereignisse) verwenden immer `id`, nie `key`.
- **D3 – Validierte Änderungen:** Eigenschaften werden nur über schemavalidierte Änderungsbefehle geschrieben, `state` nur über Transitionen.
- **D4 – Keine Schattendaten im Dokument:** Abfragbare Daten liegen in Eigenschaften. Abgeleitete Beziehungen aus dem Dokument sind eine deterministische Funktion der Dokumentversion.
- **D5 – Ein Containment-Elternteil:** Jedes Item hat höchstens ein `parent`. Berechtigungsvererbung folgt ausschliesslich dem Containment.
- **D6 – Typunabhängige Querschnittsfunktionen:** Kein typspezifischer Code in Suche, Berechtigung, Historie oder Kommentaren.

### Konsequenzen

- **Positiv:**
  - Suche, Berechtigung, Historie, Kommentare und API existieren je einmal, für alle Inhalte.
  - Neue Objektarten (Entscheidungen, Risiken, Runbooks, Testfälle) entstehen durch Konfiguration statt durch neuen Code.
  - Nahtlose Übergänge: Eine Aufgabe in einer Seite *ist* ein Item und erscheint ohne Kopie auf dem Board. Ein Issue kann einen Review-Workflow für seine Dokumentation haben.
  - Eine Abfragesprache für alle Inhalte, z. B. `type = decision AND space = PLAT AND relation(implements) -> status != done`.
  - Das Modell passt zu einem ereignisbasierten Speicher (jede Änderung ist ein benanntes Ereignis), ohne diesen bereits festzulegen.

- **Negativ / akzeptierte Nachteile:**
  - Ein generisches Modell ist schwerer zu optimieren als zwei spezialisierte. Abfragen über typspezifische Felder brauchen eine durchdachte Indizierung.
  - Typdefinitionen müssen versioniert und migrierbar sein. Das ist ein eigenes, nicht triviales Teilsystem.
  - UX-Risiko: Das Produkt darf nicht «zu generisch» wirken. Gegenmassnahme: starke eingebaute Typen mit eigener Darstellung (Seitenansicht, Issue-Ansicht, Board) auf demselben Modell.
  - Die Trennung von Dokumentkörper (Kollaborationsschicht) und Eigenschaften (Ereignisse) führt zu zwei Persistenzwegen, die konsistent zusammengeführt werden müssen.

- **Folgeentscheidungen und Risiken:**
  - ADR: Persistenz und Ereignismodell (Übernahme von Event Sourcing nach dem Muster von atlas prüfen).
  - ADR: Format des Dokumentkörpers und Wahl des CRDT/OT-Verfahrens, einschliesslich Verfügbarkeit einer ausgereiften Go-Implementierung.
  - ADR: Abfragesprache und Indizierung.
  - ADR: Berechtigungsmodell (Space-Rollen, Item-Einschränkungen, Sichtbarkeit von Beziehungen).
  - ADR: Abbildung von Confluence- und Jira-Objekten auf Typen beim Import.

## Vor- und Nachteile der Optionen

### Option 1: Zwei Domänenmodelle

Stärkste Fassung: Spezialisierte Modelle sind einfach zu verstehen, einfach zu optimieren und entsprechen dem Denkmodell bestehender Nutzer. Der Import aus Atlassian ist eine 1:1-Abbildung. Jira und Confluence beweisen seit über zwei Jahrzehnten, dass der Ansatz trägt.

- Gut: einfache, gezielt optimierbare Datenstrukturen; geringe Modellierungsrisiken; direkter Import.
- Schlecht: Jede Querschnittsfunktion existiert doppelt oder braucht eine Übersetzungsschicht. Die Integration endet an der Modellgrenze. Das ist genau der Zustand, den das Projekt überwinden soll. Ohne einheitliches Modell gäbe es keinen inhaltlichen Grund für ein neues Produkt.

### Option 2: Alles ist ein Block

Stärkste Fassung: Maximale Flexibilität. Notion zeigt, dass Nutzer Dokumente und Datenbanken in einem Modell annehmen. Transklusion und beliebige Verschachtelung sind kostenlos.

- Gut: sehr flexibel; einheitliches Bearbeitungsmodell; Seiten und Datensätze sind verlustfrei ineinander überführbar.
- Schlecht: Die Granularität ist zu fein. Absätze und Arbeitsobjekte sind gleichrangig. Dadurch sind Berechtigungen, Workflows und Abfragen auf Jira-Niveau schwer umzusetzen, und die Historie wird pro Tastendruck aufgebläht. Gerade der Workflow-Teil gilt bei Notion als Schwäche. Aus diesem Ansatz wird die Idee übernommen, dass der Dokumentkörper Items einbetten und referenzieren kann. Der Absatz selbst wird aber kein Item.

### Option 3: Generischer Graph

Stärkste Fassung: Jede Struktur ist darstellbar, Beziehungen sind erstklassig, und das Schema kann sich frei entwickeln.

- Gut: maximale Ausdruckskraft; Beziehungen als Kern.
- Schlecht: Ohne Schema gibt es keine Validierung, keine verlässlichen Formulare und keine effizienten Feldabfragen. Die Komplexität wandert in jede Anwendungsschicht. Die erstklassigen Beziehungen werden in Option 4 übernommen, die Schemalosigkeit nicht.

### Option 4: Typisiertes Item-Modell

- Gut: echte Vereinigung bei erhaltener Struktur; Querschnittsfunktionen einmalig; Typen erweiterbar; passt zu Ereignisspeicherung und Abfragesprache.
- Schlecht: höherer Entwurfsaufwand am Anfang (Typsystem, Schema-Migration, zwei Änderungsgeschwindigkeiten); Optimierung generischer Abfragen anspruchsvoller.

## Offene Fragen

1. **Ist ein Space ein Item?** Dafür spricht: Space-Startseite, Kommentare und Historie wären geschenkt. Dagegen spricht: Der Space ist Berechtigungsgrenze und Schlüssel-Namensraum, und Rekursion an dieser Stelle erschwert das Berechtigungsmodell. Vorläufig ist ein Space ein eigenständiger Container mit einem zugehörigen Start-Item.
2. **Schema-Evolution:** Wie werden bestehende Items behandelt, wenn ein Feld umbenannt, entfernt oder in seinem Typ geändert wird (lazy Migration beim Lesen oder explizite Migrationsläufe)?
3. **Typwechsel:** Darf ein Item seinen Typ ändern (Seite → Entscheidung, Task → Bug)? Wenn ja, nach welchen Regeln werden Felder abgebildet?
4. **Granularität der Dokumentversionen:** In welchen Abständen wird der Stand der Kollaborationsschicht als Version übernommen (Zeit, Inaktivität, explizites Speichern)?

## Links

- Vergleichsprojekt für Arbeitsweise und ADR-Disziplin: [pblumer/atlas](https://github.com/pblumer/atlas) (insbesondere ADR-0001, Event Sourcing)
- Folge-ADRs: Persistenz, Dokumentformat/CRDT, Abfragesprache, Berechtigungsmodell, Import (noch nicht erstellt)
