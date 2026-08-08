package dedupe

import (
	"testing"

	"github.com/j27-aurum/gofast/internal/model"
)

func TestClusterKeyBETFamilyDistinct(t *testing.T) {
	t.Parallel()
	bet := ClusterKey("BET", "Samsung")
	pluto := ClusterKey("BET Pluto TV", "Pluto")
	her := ClusterKey("BET Her", "Xumo")
	if bet == "" || pluto == "" || her == "" {
		t.Fatal("empty keys")
	}
	if bet == pluto || bet == her || pluto == her {
		t.Fatalf("BET variants must stay distinct: %q %q %q", bet, pluto, her)
	}
}

func TestClusterKeyFoldsPunctuationAndCase(t *testing.T) {
	t.Parallel()
	a := ClusterKey("Law & Crime", "LG")
	b := ClusterKey("Law&Crime · Samsung", "Samsung")
	c := ClusterKey("FOX Weather", "")
	d := ClusterKey("Fox Weather · Tubi", "Tubi")
	if a != b {
		t.Fatalf("Law&Crime fold: %q vs %q", a, b)
	}
	if c != d {
		t.Fatalf("Fox Weather fold: %q vs %q", c, d)
	}
}

func TestStripProviderSuffix(t *testing.T) {
	t.Parallel()
	if got := StripProviderSuffix("Cheaters · Samsung", "Samsung"); got != "Cheaters" {
		t.Fatalf("got %q", got)
	}
	if got := StripProviderSuffix("Cheaters · Pluto", ""); got != "Cheaters" {
		t.Fatalf("known label strip: %q", got)
	}
	if got := StripProviderSuffix("BET Pluto TV", "Pluto"); got != "BET Pluto TV" {
		t.Fatalf("must not strip brand words: %q", got)
	}
}

func TestScanMultiProviderAndBETSplit(t *testing.T) {
	t.Parallel()
	labels := map[model.ProviderID]string{
		model.ProviderSamsung: "Samsung",
		model.ProviderPluto:   "Pluto",
		model.ProviderXumo:    "Xumo",
	}
	labels[model.ProviderTubi] = "Tubi"
	chs := []model.Channel{
		{Provider: model.ProviderSamsung, ID: "1", NormalizedID: "1", Name: "Cheaters · Samsung", Region: "US"},
		{Provider: model.ProviderPluto, ID: "2", NormalizedID: "2", Name: "Cheaters · Pluto", Region: "CA"},
		{Provider: model.ProviderXumo, ID: "3", NormalizedID: "3", Name: "Cheaters · Xumo"},
		// Each BET variant on two providers — three separate clusters
		{Provider: model.ProviderSamsung, ID: "b1", NormalizedID: "b1", Name: "BET · Samsung"},
		{Provider: model.ProviderTubi, ID: "b1b", NormalizedID: "b1b", Name: "BET · Tubi"},
		{Provider: model.ProviderPluto, ID: "b2", NormalizedID: "b2", Name: "BET Pluto TV · Pluto"},
		{Provider: model.ProviderSamsung, ID: "b2b", NormalizedID: "b2b", Name: "BET Pluto TV · Samsung"},
		{Provider: model.ProviderXumo, ID: "b3", NormalizedID: "b3", Name: "BET Her · Xumo"},
		{Provider: model.ProviderTubi, ID: "b3b", NormalizedID: "b3b", Name: "BET Her · Tubi"},
		// within-provider only — not a cluster
		{Provider: model.ProviderLG, ID: "x1", NormalizedID: "x1", Name: "Only LG Twin"},
		{Provider: model.ProviderLG, ID: "x2", NormalizedID: "x2", Name: "Only LG Twin"},
	}
	clusters := Scan(chs, labels, nil)
	byKey := map[string]Cluster{}
	for _, c := range clusters {
		byKey[c.Key] = c
	}
	if len(byKey) != 4 {
		t.Fatalf("want 4 clusters (cheaters + 3 BET), got %d: %+v", len(clusters), keysOf(byKey))
	}
	cheaters := byKey[NormalizeTitle("Cheaters")]
	if len(cheaters.Members) != 3 || cheaters.Status != StatusUnresolved {
		t.Fatalf("cheaters: %+v", cheaters)
	}
	byProv := map[model.ProviderID]Member{}
	for _, m := range cheaters.Members {
		byProv[m.Provider] = m
	}
	if byProv[model.ProviderSamsung].Region != "US" || byProv[model.ProviderPluto].Region != "CA" {
		t.Fatalf("region not plumbed: %+v", cheaters.Members)
	}
	if _, ok := byKey[NormalizeTitle("BET")]; !ok {
		t.Fatal("missing BET cluster")
	}
	if _, ok := byKey[NormalizeTitle("BET Pluto TV")]; !ok {
		t.Fatal("missing BET Pluto TV cluster")
	}
	if _, ok := byKey[NormalizeTitle("BET Her")]; !ok {
		t.Fatal("missing BET Her cluster")
	}
}

func TestScanResolvedWhenOneExportable(t *testing.T) {
	t.Parallel()
	labels := map[model.ProviderID]string{model.ProviderSamsung: "Samsung", model.ProviderPluto: "Pluto"}
	chs := []model.Channel{
		{Provider: model.ProviderSamsung, ID: "1", NormalizedID: "1", Name: "Nosey", Excluded: false},
		{Provider: model.ProviderPluto, ID: "2", NormalizedID: "2", Name: "Nosey", Excluded: true, FilterReason: model.FilterReasonEmitDisabled},
	}
	clusters := Scan(chs, labels, nil)
	if len(clusters) != 1 || clusters[0].Status != StatusResolved || clusters[0].ExportableCount != 1 {
		t.Fatalf("%+v", clusters)
	}
}

func TestPickPreferred(t *testing.T) {
	t.Parallel()
	members := []Member{
		{Provider: model.ProviderPlex, Exportable: true},
		{Provider: model.ProviderSamsung, Exportable: true},
		{Provider: model.ProviderPluto, Exportable: false},
	}
	i := PickPreferred(members, []model.ProviderID{model.ProviderSamsung, model.ProviderPluto, model.ProviderPlex})
	if i != 1 {
		t.Fatalf("want samsung index 1, got %d", i)
	}
	if PickPreferred(members, nil) != -1 {
		t.Fatal("empty preferred")
	}
}

func TestDocNormalized(t *testing.T) {
	t.Parallel()
	d := Doc{
		PreferredProviders: []model.ProviderID{"Samsung", "samsung", " pluto ", ""},
		KeepAllKeys:        []string{" cheaters ", "cheaters", ""},
	}.Normalized()
	if len(d.PreferredProviders) != 2 || d.PreferredProviders[0] != "samsung" || d.PreferredProviders[1] != "pluto" {
		t.Fatalf("%+v", d.PreferredProviders)
	}
	if len(d.KeepAllKeys) != 1 || d.KeepAllKeys[0] != "cheaters" {
		t.Fatalf("%+v", d.KeepAllKeys)
	}
}

func keysOf(m map[string]Cluster) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
