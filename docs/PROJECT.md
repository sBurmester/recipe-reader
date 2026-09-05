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
  - Datenschutzbestimmungen

## 7. Aufgaben für die KI
- Generierung des Codes für die Rezept-Extraktion
- Generierung des Codes für die Rezept-Speicherung
- Generierung des Codes für die Rezept-Anzeige

## 8. Anhang
- github.com/felipeinf/instago
