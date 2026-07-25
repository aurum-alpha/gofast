package categories_test

import (
	"testing"

	"github.com/j27-aurum/gofast/internal/categories"
	"github.com/j27-aurum/gofast/internal/model"
)

func TestApplyMapsAndDedupes(t *testing.T) {
	p := categories.Compile(categories.Doc{
		Enabled: true,
		Merges: []categories.Merge{
			{Name: "Movie", Members: []string{"Movie", "Movies", "Film"}},
		},
	})
	progs := []model.Programme{{
		Categories: []string{"Movies", "Film", "Comedy"},
	}}
	out := categories.Apply(progs, p)
	got := out[0].EmittedCategories
	if len(got) != 2 || got[0] != "Movie" || got[1] != "Comedy" {
		t.Fatalf("EmittedCategories=%v", got)
	}
	if len(out[0].Categories) != 3 {
		t.Fatal("upstream Categories must be untouched")
	}
}

func TestApplyNoOpWhenDisabled(t *testing.T) {
	p := categories.Compile(categories.Doc{
		Enabled: false,
		Merges:  []categories.Merge{{Name: "Movie", Members: []string{"Movies"}}},
	})
	progs := []model.Programme{{Categories: []string{"Movies"}}}
	out := categories.Apply(progs, p)
	if len(out[0].EmittedCategories) != 0 {
		t.Fatalf("want empty EmittedCategories when off, got %v", out[0].EmittedCategories)
	}
}
