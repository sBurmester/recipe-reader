// web/src/main.ts
//
// Frontend composition root, mirroring internal/server on the backend: build
// the chrome once, then dispatch the hash to a page renderer. A hash router
// needs no server-side rewrite rules, which matters because the Go binary
// serves the built assets from a single embedded directory.

import { el } from "./dom";
import { renderDetailPage } from "./pages/detail";
import { renderImportPage } from "./pages/import";
import { renderListPage } from "./pages/list";

const app = document.getElementById("app");
if (!app) throw new Error("main: #app container missing from index.html");

document.body.prepend(
  el("nav", {}, [
    el("a", { href: "#/" }, ["Rezepte"]),
    el("a", { href: "#/import" }, ["Import"]),
  ]),
);

const content = el("main");
app.append(content);

function route(): void {
  const hash = window.location.hash || "#/";
  const detailMatch = hash.match(/^#\/recipes\/(\d+)$/);

  if (hash === "#/import") {
    renderImportPage(content);
  } else if (detailMatch) {
    renderDetailPage(content, Number(detailMatch[1]));
  } else {
    // Everything else falls back to the list rather than a 404 page: the only
    // ways in are the two nav links and the cards' own hrefs.
    renderListPage(content);
  }
}

window.addEventListener("hashchange", route);
route();
