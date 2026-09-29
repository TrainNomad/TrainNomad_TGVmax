package main

import (
	"os"
	"testing"
	"time"
)

// Tests lancés par la GitHub Action avant de publier network.bin.gz : le fichier doit se charger
// et le moteur doit trouver des trajets TGVmax cohérents.

var testEngine *Engine

func engineForTest(tb testing.TB) *Engine {
	if testEngine != nil {
		return testEngine
	}
	path := findNetwork()
	if _, err := os.Stat(path); err != nil {
		tb.Skip("network.bin absent")
	}
	n, err := LoadNetwork(path)
	if err != nil {
		tb.Fatal(err)
	}
	testEngine = NewEngine(n)
	return testEngine
}

func searchAt(tb testing.TB, e *Engine, from, to string, date string, hh, mm, limit int) []rawJourney {
	f, t := e.Places.Resolve(from), e.Places.Resolve(to)
	if f == nil || t == nil {
		tb.Fatalf("lieu inconnu %s / %s", from, to)
	}
	loc := e.Net.Locations[e.Net.StopTZ[f.stops[0]]]
	d, _ := time.ParseInLocation("2006-01-02", date, loc)
	start := time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, loc)
	return e.Search(f, t, e.Net.DayIndex(d), int32(start.Sub(e.Net.BaseDate)/time.Minute), limit, 6)
}

// Les places TGVmax partent vite : on teste des dates éloignées, où il en reste davantage.
func testDates(e *Engine) []string {
	var out []string
	for d := e.Net.LastDate(); !d.Before(e.Net.FirstDate()) && len(out) < 5; d = d.AddDate(0, 0, -3) {
		out = append(out, d.Format("2006-01-02"))
	}
	return out
}

func TestNetworkLoaded(t *testing.T) {
	e := engineForTest(t)
	if e.Net.NumStops() < 100 || len(e.Net.TripDays) < 1000 {
		t.Fatalf("réseau trop petit : %d gares, %d trajets", e.Net.NumStops(), len(e.Net.TripDays))
	}
	for _, q := range []string{"Paris", "Lyon", "Marseille", "Bordeaux"} {
		if e.Places.Resolve(q) == nil {
			t.Errorf("%s introuvable", q)
		}
	}
}

// Chaque trajet renvoyé doit être cohérent ; au moins une grande relation doit avoir des trajets.
func TestSearchConsistency(t *testing.T) {
	e := engineForTest(t)
	pairs := [][2]string{
		{"Paris", "Lyon"}, {"Paris", "Marseille"}, {"Paris", "Bordeaux"}, {"Paris", "Rennes"},
		{"Lille", "Lyon"}, {"Strasbourg", "Paris"}, {"Nantes", "Paris"}, {"Paris", "Montpellier"},
	}
	found := 0
	for _, date := range testDates(e) {
		for _, p := range pairs {
			js := searchAt(t, e, p[0], p[1], date, 5, 0, 10)
			found += len(js)
			for _, j := range js {
				if j.arr <= j.dep || j.trains < 1 || j.trains > 7 {
					t.Errorf("%s %s -> %s : trajet incohérent %+v", date, p[0], p[1], j)
				}
			}
		}
	}
	if found == 0 {
		t.Fatal("aucun trajet TGVmax trouvé sur les grandes relations")
	}
	t.Logf("%d trajets trouvés", found)
}

func TestExplorer(t *testing.T) {
	e := engineForTest(t)
	f := e.Places.Resolve("Paris")
	total := 0
	for _, date := range testDates(e) {
		d, _ := time.ParseInLocation("2006-01-02", date, e.Net.Locations[0])
		t0 := int32(d.Sub(e.Net.BaseDate) / time.Minute)
		total += len(e.Explore(f, e.Net.DayIndex(d), t0, t0+24*60, 2))
	}
	if total == 0 {
		t.Fatal("aucune destination depuis Paris")
	}
}

func BenchmarkSearch(b *testing.B) {
	e := engineForTest(b)
	date := testDates(e)[0]
	searchAt(b, e, "Paris", "Marseille", date, 6, 0, 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		searchAt(b, e, "Paris", "Marseille", date, 6, 0, 10)
		searchAt(b, e, "Lille", "Bordeaux", date, 6, 0, 10)
	}
}
