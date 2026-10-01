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
	return e.Search(f, t, e.Net.DayIndex(d), int32(start.Sub(e.Net.BaseDate)/time.Minute), limit, 6, false)
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

// Une recherche depuis une ville (toutes ses gares) ne doit perdre aucun train direct trouvé depuis
// l'une de ses gares, sauf s'il est dominé : départ plus tard et arrivée plus tôt (avec au plus
// transferPenalty minutes de gagnées par correspondance supplémentaire, cf. targetBound).
// Ex. Paris → Nice : l'Intercités de nuit d'Austerlitz, plus long que les TGV de Gare de Lyon.
func TestCityKeepsStationDirects(t *testing.T) {
	e := engineForTest(t)
	city := e.Places.Resolve("Paris")
	if city == nil || city.Type != "city" {
		t.Skip("Paris n'est pas une ville à plusieurs gares dans ce réseau")
	}
	dominated := func(j rawJourney, js []rawJourney) bool {
		for _, o := range js {
			gain := int32(0)
			if o.trains > j.trains {
				gain = transferPenalty * int32(o.trains-j.trains)
			}
			if o.dep >= j.dep && o.arr+gain <= j.arr {
				return true
			}
		}
		return false
	}
	checked := 0
	for d := e.Net.FirstDate(); !d.After(e.Net.LastDate()); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		for _, dest := range []string{"Nice", "Marseille", "Toulouse", "Bordeaux", "Lyon", "Rennes", "Strasbourg", "Lille"} {
			cityJs := searchAt(t, e, "Paris", dest, date, 0, 0, 50)
			for _, s := range city.stops {
				for _, j := range searchAt(t, e, "station:"+e.Net.StopID[s], dest, date, 0, 0, 50) {
					if j.trains != 1 {
						continue
					}
					checked++
					if !dominated(j, cityJs) {
						t.Errorf("%s Paris -> %s : train direct %s (%d -> %d) absent de la recherche ville",
							date, dest, e.Net.StopName[s], j.dep, j.arr)
					}
				}
			}
		}
	}
	t.Logf("%d trains directs vérifiés", checked)
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

// Changement de siège : deux billets TGVmax successifs dans le même train, sans temps de correspondance.
// Chaque changement de siège doit se faire dans la même gare, avec le même numéro de train.
func TestSeatChange(t *testing.T) {
	e := engineForTest(t)
	if e.Net.Meta.SeatChangeMax == 0 {
		t.Skip("changement de siège désactivé dans ce réseau")
	}
	cities := []string{"Paris", "Lyon", "Marseille", "Bordeaux", "Lille", "Rennes", "Nantes", "Montpellier",
		"Strasbourg", "Toulouse", "Nice", "Avignon", "Grenoble", "Dijon", "Tours", "Le Mans"}
	found := 0
	for _, date := range testDates(e) {
		for _, a := range cities {
			for _, b := range cities {
				if a == b {
					continue
				}
				f, to := e.Places.Resolve(a), e.Places.Resolve(b)
				d, _ := time.ParseInLocation("2006-01-02", date, e.Net.Locations[0])
				day := e.Net.DayIndex(d)
				dt := e.tables.get(day)
				for _, j := range e.Search(f, to, day, int32(d.Sub(e.Net.BaseDate)/time.Minute), 10, 3, false) {
					var prev *rawLeg
					for li := range j.legs {
						l := &j.legs[li]
						if l.walk {
							prev = nil
							continue
						}
						if l.seat {
							found++
							trip := func(l *rawLeg) uint32 { return dt.InstTrip[dt.InstOff[l.route]+uint32(l.inst)] }
							if prev == nil || e.Net.TripNumber[trip(prev)] != e.Net.TripNumber[trip(l)] ||
								e.Net.routeStops(prev.route)[prev.alight] != e.Net.routeStops(l.route)[l.board] {
								t.Errorf("%s %s -> %s : changement de siège incohérent", date, a, b)
							}
							if found == 1 {
								t.Logf("exemple : %s %s -> %s, train %s, changement de siège à %s", date, a, b,
									e.Net.TripNumber[trip(l)], e.Net.StopName[e.Net.routeStops(l.route)[l.board]])
							}
						}
						prev = l
					}
				}
			}
		}
	}
	t.Logf("%d changements de siège trouvés", found)
}
