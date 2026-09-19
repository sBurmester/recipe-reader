# Projektname: Recipe Reader

## 1. Zusammenfassung
Das Projekt soll ein Rezeptscanner sein, der von Social Media Plattformen wie Instagram und Co. Rezepte aus der Caption extrahiert, diese in eine strukturierte Form überführt und diese dann in eine Datenbank speichert. Diese Datenbank soll dann von einem Webserver ausgelesen werden können, um Rezepte zu suchen und anzuzeigen. Die Rezepte sollen in der Webanwendung auch gesucht, gefiltert und bearbeitet werden können.

## 2. Zielsetzung
- Auslesen der Caption von Instagram Posts
- Im eigenen Profil gespeicherte Posts, am besten aus einer Collection, sollen ausgelesen werden
- Extrahieren der Rezepte aus der Caption
- Speichern der Rezepte in einer Datenbank
- Anzeigen der Rezepte in einer Webanwendung
- Suchen, Filtern und Bearbeiten der Rezepte in der Webanwendung

## 3. Kontext und Hintergrund
### 3.1 Hintergrund
 - Es gibt keine bestehende Lösung, die Rezepte aus Social Media Plattformen extrahiert und in einer Datenbank speichert.
### 3.2 Kontext
- Zielgruppe: Kochen-Enthusiasten, die Rezepte aus Social Media Plattformen extrahieren und speichern möchten.
- Anwendungsbereich: Kochen, Ernährung, Rezeptsammlung

## 4. Technische Details
- Programmiersprachen: Golang(Backend und Rezpet-Extraktion) 1.27.1, TypeScript(Frontend) 5.3.3, HTML, CSS
- Datenbank: SQLite, PostgresQL, MySQL
- APIs: Instagram API, Webserver API
- Tools: Git, GitHub, Zed, Docker

### 4.1 Architektur
- Client-Server-Architektur
- Single Binary Application
- 

## 5. Projektstruktur
### 5.1 Hauptkomponenten
- Webserver
- Datenbank
- Instagram API Client (mit instago)
- Rezept-Extraktion
- Rezept-Speicherung
- Rezept-Anzeige

### 5.2 Datenfluss
1. Instagram API Client holt Rezepte von Instagram
2. Rezept-Extraktion extrahiert die Rezepte aus den Instagram Posts
3. Rezept-Speicherung speichert die Rezepte in der Datenbank
4. Rezept-Anzeige zeigt die Rezepte in der Webanwendung an

### 5.3 Datenbank-Schema
- Rezept
  - ID
  - Name
  - Zutaten
    - ID
    - Zutat-ID
    - Menge
    - Einheit-ID
  - Kategorien
    - ID
    - Name
  - Zubereitung
  - Bild
  - Quelle
  - Erstellungsdatum
  - Bearbeitungsdatum
- Kategorie
  - ID
  - Name
- Zutat
  - ID
  - Name
  - Einheit
- Einheit
  - ID
  - Name

## 6. Anforderungen und Einschränkungen
- Performance-Anforderungen
  - Rezept-Extraktion
    - muss nicht in Echtzeit erfolgen
    - kann in Hintergrundprozessen erfolgen
  - Rezept-Speicherung
     - muss nicht in Echtzeit erfolgen
     - kann in Hintergrundprozessen erfolgen
  - Rezept-Anzeige
    - muss in Echtzeit erfolgen
    - muss schnell sein
- Ressourcen-Limitierungen
  - Instagram API Rate Limits
  - Datenbank-Größe
- Datenschutz oder ethische Überlegungen
  - Instagram API Nutzungsbedingungen
  - Datenschutzbestimmungen — siehe 6.1

### 6.1 Datenhaltung und Herkunft der importierten Inhalte

Festgehalten als Entscheidung zu Review-Finding `security` S8 (Aufgabe T-57).
Der Import speichert Inhalte anderer Personen: den Caption-Text als
Zubereitung, den Permalink des Posts als `source` und die CDN-Adresse des Bildes
als `image_url`. Captions können Namen und Handles Dritter enthalten; es sind
personenbezogene Daten, die nicht von der Nutzerin oder dem Nutzer selbst
stammen.

**Zweck: eine private Rezeptsammlung für eine Person.** Importiert werden nur
Posts, die das eigene Konto gespeichert hat. Die Sammlung wird nicht
veröffentlicht, nicht geteilt und nicht weiterverbreitet. Alles Weitere in
diesem Abschnitt gilt unter dieser Annahme; fällt sie weg, gilt der letzte
Absatz.

- **Herkunft.** `source` ist bei jedem importierten Rezept gesetzt, eindeutig
  und bleibt beim Bearbeiten erhalten. Es ist die Quellenangabe und zugleich der
  Schlüssel, an dem der Import Duplikate erkennt — es wird nicht entfernt.
- **Aufbewahrung.** Es gibt keine automatische Löschung. Ein Rezept bleibt, bis
  es in der Oberfläche (oder per `DELETE /api/recipes/{id}`) gelöscht wird. Wird
  ein Post auf Instagram gelöscht oder aus der Sammlung entfernt, bleibt das
  Rezept bestehen: der Import fügt nur hinzu. Das ist für eine private Sammlung
  gewollt — eine Rezeptsammlung, die sich selbst leert, wenn Dritte etwas
  löschen, erfüllt ihren Zweck nicht.
- **Bilder.** `image_url` wird gespeichert, aber nirgends angezeigt. Das bleibt
  so, bis es bewusst anders entschieden wird: ein angezeigtes Bild würde bei
  jedem Seitenaufruf aus dem Browser Instagrams CDN abfragen. Die
  Content-Security-Policy des Frontends (T-48) lässt externe Bilder deshalb
  nicht zu; wer sie anzeigen will, muss dort `img-src` ändern.
- **Lesezugriff ist Teilen.** Schreibende Anfragen verlangen ein `API_TOKEN`,
  lesende nicht (Entscheidung zu `security` S1). Eine Instanz, die über das
  Netz erreichbar ist — die Compose-Datei veröffentlicht Port 8080 auf allen
  Schnittstellen —, macht die ganze Sammlung für jeden lesbar, der den Port
  erreicht. Für den privaten Zweck gehört sie hinter `127.0.0.1`, ein VPN oder
  einen Reverse-Proxy mit Anmeldung.

**Falls die Sammlung je geteilt oder veröffentlicht werden soll**, trägt die
Annahme oben nicht mehr, und vorher ist mindestens nötig: die Quelle (`source`)
in der Oberfläche sichtbar als Quellenangabe anzeigen; eine Regel für
Aufbewahrung und Löschung festlegen und umsetzen; Lesezugriffe authentifizieren;
und klären, ob die Verfasserinnen und Verfasser der Posts der Weitergabe
zustimmen müssen. Das ist eine rechtliche Frage, die dieses Dokument nicht
beantwortet.

## 7. Aufgaben für die KI
- Generierung des Codes für die Rezept-Extraktion
- Generierung des Codes für die Rezept-Speicherung
- Generierung des Codes für die Rezept-Anzeige

## 8. Anhang
- github.com/felipeinf/instago
