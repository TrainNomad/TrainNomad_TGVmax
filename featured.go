package main

import (
	"hash/fnv"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

// Trajets mis en avant sur la page d'accueil TGVmax : quelques trajets directs tirés au sort parmi
// les départs des prochains jours. Le tirage ne dépend que de la date du jour et du réseau chargé :
// la sélection reste la même toute la journée et change avec la mise à jour quotidienne des données.

const (
	featuredCount   = 4
	featuredMinDur  = 90     // trajets assez longs pour donner envie de partir
	featuredMaxDur  = 6 * 60 // pas de journée entière dans le train
	featuredFirstH  = 7      // départs en journée uniquement
	featuredLastH   = 20
	featuredMaxDays = 3 // J+1 à J+3
)

// Villes de départ possibles, réparties sur tout le territoire.
var featuredOrigins = []string{
	"Paris", "Lyon", "Marseille", "Bordeaux", "Lille", "Nantes", "Strasbourg",
	"Montpellier", "Rennes", "Toulouse", "Nice", "St-Malo", "Angers",
}

type featuredCache struct {
	mu   sync.Mutex
	key  string
	resp map[string]any
}

var featured featuredCache

// placeCity : nom de ville d'un lieu (pour éviter deux trajets vers la même ville).
func placeCity(p *Place) string {
	if p.Type == "city" || p.City == "" {
		return p.Name
	}
	return p.City
}

func (s *Server) handleFeatured(w http.ResponseWriter, r *http.Request) {
	n := s.e.Net
	loc := n.Locations[0]
	if paris, err := time.LoadLocation("Europe/Paris"); err == nil {
		loc = paris
	}
	today := time.Now().In(loc)
	key := today.Format("2006-01-02") + "|" + n.Meta.BuiltAt

	featured.mu.Lock()
	defer featured.mu.Unlock()
	if featured.key != key {
		featured.key, featured.resp = key, s.pickFeatured(today, key)
	}
	writeJSON(w, r, http.StatusOK, featured.resp)
}

func (s *Server) pickFeatured(today time.Time, seed string) map[string]any {
	n := s.e.Net
	h := fnv.New64a()
	h.Write([]byte(seed))
	rng := rand.New(rand.NewSource(int64(h.Sum64())))

	var days []time.Time
	for i := 1; i <= featuredMaxDays; i++ {
		d := time.Date(today.Year(), today.Month(), today.Day()+i, 0, 0, 0, 0, today.Location())
		if !d.After(n.LastDate()) {
			days = append(days, d)
		}
	}

	journeys := []Journey{}
	usedFrom, usedTo := map[string]bool{}, map[string]bool{}
	origins := append([]string(nil), featuredOrigins...)
	rng.Shuffle(len(origins), func(i, j int) { origins[i], origins[j] = origins[j], origins[i] })

	for slot := 0; slot < featuredCount*3 && len(journeys) < featuredCount && len(days) > 0; slot++ {
		from := s.e.Places.Resolve(origins[slot%len(origins)])
		if from == nil || usedFrom[placeCity(from)] {
			continue
		}
		// jours répartis : chaque trajet tombe sur l'un des prochains jours, dans l'ordre
		d := days[len(journeys)%len(days)]
		day := n.DayIndex(d)
		tStart := s.minutes(d.Add(featuredFirstH * time.Hour))
		tEnd := s.minutes(d.Add(featuredLastH * time.Hour))

		var cands []Destination
		for _, dest := range s.e.Explore(from, day, tStart, tEnd, 0) {
			if dest.Direct == nil || dest.Direct.DurationMin < featuredMinDur || dest.Direct.DurationMin > featuredMaxDur {
				continue
			}
			if c := placeCity(dest.Place); c == placeCity(from) || usedTo[c] {
				continue
			}
			cands = append(cands, dest)
		}
		if len(cands) == 0 {
			continue
		}
		dest := cands[rng.Intn(len(cands))]
		dep, err := time.Parse("2006-01-02T15:04:05-07:00", dest.Direct.Departure)
		if err != nil {
			continue
		}
		raw := s.e.Search(from, dest.Place, day, s.minutes(dep), 1, 0, false)
		if len(raw) == 0 {
			continue
		}
		journeys = append(journeys, s.toJourney(s.e.tables.get(day), raw[0]))
		usedFrom[placeCity(from)] = true
		usedTo[placeCity(dest.Place)] = true
	}

	return map[string]any{
		"date":     today.Format("2006-01-02"),
		"built_at": n.Meta.BuiltAt,
		"count":    len(journeys),
		"journeys": journeys,
	}
}
