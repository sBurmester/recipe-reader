package repository

import (
	"context"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/domain"
)

// % and _ are LIKE metacharacters, not literals, and the search box hands them
// straight to the pattern. Before the escaping, searching for "50%" matched
// every recipe and "a_b" matched "axb" — and CountRecipes got it wrong the same
// way, so the total disagreed with the page.
func TestRecipeRepository_Search_TreatsLikeMetacharactersAsLiterals(t *testing.T) {
	ctx := context.Background()
	repo := newTestRecipeRepo(t)

	names := []string{
		"50% Vollkornbrot", // the only match for "50%"
		"Bananenbrot",
		"Apfelkuchen",
		"Zucker_Zimt-Sterne", // the only match for "r_z"
		"Zuckerzimtsterne",   // would match "r_z" if _ stayed a wildcard
		`Rustikales \ Bauernbrot`,
	}
	for _, name := range names {
		r := &domain.Recipe{Name: name, Source: "src-" + name, Status: domain.StatusPublished}
		if err := repo.Create(ctx, r); err != nil {
			t.Fatalf("Create(%q) error = %v", name, err)
		}
	}

	tests := []struct {
		query string
		want  string
	}{
		{query: "50%", want: "50% Vollkornbrot"},
		{query: "r_z", want: "Zucker_Zimt-Sterne"},
		{query: `s \ b`, want: `Rustikales \ Bauernbrot`},
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			results, total, err := repo.Search(ctx, SearchQuery{Text: tc.query, PageSize: 100})
			if err != nil {
				t.Fatalf("Search(%q) error = %v", tc.query, err)
			}
			if len(results) != 1 || results[0].Name != tc.want {
				t.Fatalf("Search(%q) returned %d rows (%+v), want only %q",
					tc.query, len(results), recipeNames(results), tc.want)
			}
			// The count runs the same predicate in a second query. Escaping in
			// one and not the other is the drift the SQL-side fix avoids, and
			// it would show up here as a total that disagrees with the page.
			if total != 1 {
				t.Errorf("Search(%q) total = %d, want 1 — the count disagrees with the page", tc.query, total)
			}
		})
	}
}

// A bare wildcard is a literal too: nothing in the UI promises pattern syntax,
// so "%" finds the one recipe with a percent sign in its name.
func TestRecipeRepository_Search_BareWildcardMatchesLiterally(t *testing.T) {
	ctx := context.Background()
	repo := newTestRecipeRepo(t)

	for _, name := range []string{"50% Vollkornbrot", "Bananenbrot", "Apfelkuchen"} {
		r := &domain.Recipe{Name: name, Source: "src-" + name, Status: domain.StatusPublished}
		if err := repo.Create(ctx, r); err != nil {
			t.Fatalf("Create(%q) error = %v", name, err)
		}
	}

	results, total, err := repo.Search(ctx, SearchQuery{Text: "%", PageSize: 100})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if total != 1 || len(results) != 1 {
		t.Fatalf("Search(%%) returned %d rows (total %d), want only the one with a percent sign: %+v",
			len(results), total, recipeNames(results))
	}
}

func recipeNames(recipes []domain.Recipe) []string {
	names := make([]string, 0, len(recipes))
	for _, r := range recipes {
		names = append(names, r.Name)
	}
	return names
}
