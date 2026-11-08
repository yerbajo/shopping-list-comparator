// Package identity decides whether a product of one chain is the same thing as the
// product the shopper added to the list, and says how sure it is.
//
// The ladder is: same EAN, then the same product family (the shopper's words, measured on
// the product's head noun), then the same typed variant axes, then the same size. A
// candidate that passes all of it is `equivalente`; a candidate that is the right family
// but a different variant, an unknown size or another format is `probable`; a candidate
// from another domain is `no`. Nothing here invents a price or a product: a chain that
// publishes no size can never be `equivalente`, and the reason says so.
//
// The own-brand lexicon is data (brands.json), never code: it grows every time a chain
// launches a line, and DIA alone names a different sub-brand per category while MAS
// publishes no brand field at all. The lexicon therefore has three independent rules —
// a prefix on the brand, a pattern on the brand, a pattern on the name — because the three
// chains that publish a brand are not the three chains the shopper names.
package identity

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Product is one product a chain sells. PriceCents is nil when the chain publishes no
// price, and nil is "not published", never zero.
type Product struct {
	Store      string `json:"store"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	Brand      string `json:"brand,omitempty"`
	Size       string `json:"size,omitempty"`
	Category   string `json:"category,omitempty"`
	EAN        string `json:"ean,omitempty"`
	URL        string `json:"url,omitempty"`
	Image      string `json:"image,omitempty"`
	PriceCents *int64 `json:"priceCents,omitempty"`
}

// Levels, from the strongest claim to the weakest. `no` is a real answer: it says the
// catalogue was searched and nothing there is that product.
const (
	LevelExact = "exacta"
	LevelSame  = "equivalente"
	LevelMaybe = "probable"
	LevelNo    = "no"
	// LevelPicked is not a judgement: it is the product the shopper chose for that chain,
	// which outranks any heuristic here and is counted in that chain's basket.
	LevelPicked = "elegida"
)

// Match is one candidate judged against the anchor of a list item.
type Match struct {
	Product Product `json:"product"`
	Level   string  `json:"level"`
	Reason  string  `json:"reason,omitempty"`
	Score   int     `json:"score"`
}

// ---------- normalisation ----------

var accents = strings.NewReplacer(
	"á", "a", "à", "a", "ä", "a", "â", "a", "ã", "a",
	"é", "e", "è", "e", "ë", "e", "ê", "e",
	"í", "i", "ì", "i", "ï", "i", "î", "i",
	"ó", "o", "ò", "o", "ö", "o", "ô", "o", "õ", "o",
	"ú", "u", "ù", "u", "ü", "u", "û", "u",
	"ñ", "n", "ç", "c",
)

// fold lowercases, strips accents and the trademark marks chains print, and collapses
// whitespace. "MILSANI®" and "Milsani" must be the same word.
func fold(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer("®", " ", "™", " ", "©", " ").Replace(s)
	s = accents.Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

var stopwords = map[string]bool{"de": true, "del": true, "la": true, "el": true,
	"los": true, "las": true, "con": true, "y": true, "en": true, "para": true,
	"al": true, "un": true, "una": true, "x": true}

// tokens is the set of content words and numbers in s, plural-folded, split on punctuation
// and on the letter/digit boundary so a sold size is compared and not guessed. Spanish
// plural matters: every chain titles a packet "Galletas" while the shopper types
// "galletas maria", and losing that match costs a whole chain.
func tokens(s string) []string {
	r := []rune(fold(s))
	out := make([]string, 0, len(r)/4)
	var cur []rune
	flush := func() {
		if len(cur) == 0 {
			return
		}
		w := string(cur)
		cur = cur[:0]
		if stopwords[w] {
			return
		}
		out = append(out, singular(w))
	}
	prevDigit := false
	for _, c := range r {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) {
			flush()
			continue
		}
		d := unicode.IsDigit(c)
		if len(cur) > 0 && d != prevDigit {
			flush()
		}
		prevDigit = d
		cur = append(cur, c)
	}
	flush()
	return out
}

// singular folds the plural of a Spanish word enough to compare names: "galletas" ->
// "galleta", "huevos" -> "huevo". It is deliberately crude — a stemmer would be a second
// language model inside the matcher — and it only applies to words long enough that the
// trailing s cannot be part of the noun itself.
func singular(w string) string {
	if len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") {
		return w[:len(w)-1]
	}
	return w
}

func tokenSet(s string) map[string]bool {
	t := tokens(s)
	m := make(map[string]bool, len(t))
	for _, w := range t {
		m[w] = true
	}
	return m
}

// ---------- own brands ----------

type brandRules struct {
	Prefix []string `json:"prefix"`
	Brand  []string `json:"brand"`
	Name   []string `json:"name"`
}

//go:embed brands.json
var brandsJSON []byte

var brands = loadBrands()

func loadBrands() map[string]brandRules {
	var m map[string]brandRules
	if err := json.Unmarshal(brandsJSON, &m); err != nil {
		// The lexicon is embedded at build time: a broken one is a build defect, not a
		// runtime condition the app can recover from.
		panic("identity: brands.json is not valid: " + err.Error())
	}
	return m
}

// OwnBrand says whether a product carries its chain's own brand. The pointer separates
// the three answers that matter: true, false, and nil for "this chain publishes no brand
// and the name gives it away for neither" — which is the normal case on MAS and must never
// be reported as a different brand.
func OwnBrand(p Product) *bool {
	rules, ok := brands[p.Store]
	if !ok {
		return nil
	}
	b, n := fold(p.Brand), fold(p.Name)
	for _, pre := range rules.Prefix {
		if p.Brand != "" && strings.HasPrefix(b, fold(pre)) {
			return boolp(true)
		}
	}
	for _, pat := range rules.Brand {
		if p.Brand != "" && strings.Contains(b, fold(pat)) {
			return boolp(true)
		}
	}
	// The name is checked even when a brand field exists: on Carrefour the field carries a
	// label ("Blanco sin corteza maxi") while the chain's own brand is in the name.
	for _, pat := range rules.Name {
		if strings.Contains(n, fold(pat)) {
			return boolp(true)
		}
	}
	if p.Brand != "" {
		return boolp(false)
	}
	return nil
}

func boolp(v bool) *bool { return &v }

// ---------- domain ----------

// domainRule maps the words a chain prints to one product domain. It is a gate, not an
// identity: two chips of the same brand are still different products, and the four false
// positives measured on the real catalogues are exactly this mistake — "cuecehuevos" for
// eggs, "escobilla" for toilet paper, "anchoas en aceite de girasol" for sunflower oil and
// "molde de silicona" for sliced bread.
type domainRule struct {
	name string
	word string
}

var domainRules = []domainRule{
	{"mascotas", "perro"}, {"mascotas", "gato"}, {"mascotas", "mascota"},
	{"hogar", "cuecehuevo"}, {"hogar", "cebador"}, {"hogar", "escob"},
	{"hogar", "aspirador"}, {"hogar", "sarten"}, {"hogar", "cafetera"},
	{"hogar", "arrocera"}, {"hogar", "molde para"}, {"hogar", "molde de silicona"},
	{"hogar", "juego de molde"}, {"hogar", "utensilio"}, {"hogar", "microondas"},
	{"hogar", "freidora"}, {"hogar", "bombilla"}, {"hogar", "alfombra"},
	{"higiene", "papel higienic"}, {"higiene", "panal"}, {"higiene", "champu"},
	{"higiene", "jabon"}, {"higiene", "desodorante"}, {"higiene", "cepillo de diente"},
	{"drogueria", "detergente"}, {"drogueria", "suavizante"}, {"drogueria", "lejia"},
	{"drogueria", "lavavajillas"}, {"drogueria", "limpiador"}, {"drogueria", "friegasuelo"},
	{"conservas", "conserva"}, {"conservas", "anchoa"}, {"conservas", "sardina"},
	{"conservas", "sardinilla"}, {"conservas", "atun"}, {"conservas", "caballa"},
	{"conservas", "mejillone"}, {"conservas", "calamar"},
	{"bebidas", "bebida"}, {"bebidas", "refresco"}, {"bebidas", "zumo"},
	{"bebidas", "cerveza"}, {"bebidas", "vino"}, {"bebidas", "agua mineral"},
	{"galletas", "galleta"},
	{"dulces", "chocolate"}, {"dulces", "bombon"}, {"dulces", "turron"},
	{"dulces", "golosina"}, {"dulces", "caramelo"}, {"dulces", "helado"},
	{"lacteos", "leche"}, {"lacteos", "yogur"}, {"lacteos", "queso"},
	{"lacteos", "nata"}, {"lacteos", "mantequilla"}, {"lacteos", "kefir"},
	{"lacteos", "cuajada"}, {"lacteos", "batido"},
	{"huevos", "huevo"},
	{"pan", "pan"}, {"pan", "tostada"}, {"pan", "bolleria"},
	{"arroces", "arroz"},
	{"aceites", "aceite"},
	{"salsas", "tomate frito"}, {"salsas", "salsa"}, {"salsas", "ketchup"},
	{"salsas", "mayonesa"},
	{"congelados", "congelado"}, {"congelados", "ultracongelado"},
	{"cafe", "cafe"}, {"cafe", "capsula"},
}

var domainLabels = map[string]string{
	"mascotas": "comida de mascotas", "hogar": "menaje de hogar", "higiene": "higiene",
	"drogueria": "droguería", "conservas": "conservas", "bebidas": "bebidas",
	"lacteos": "lácteos", "huevos": "huevos", "pan": "pan", "arroces": "arroz",
	"aceites": "aceites", "salsas": "salsas", "galletas": "galletas",
	"congelados": "congelados", "cafe": "café",
}

// Domain returns one canonical domain for a product, or "" when nothing matches. Both the
// category the chain publishes and the words in the name are evidence: a chain's taxonomy
// is coarser than the product ("Food" on LIDL) and a name is noisier (a mould for bread
// says "pan"), so either one alone would be blind somewhere.
func Domain(p Product) string {
	// The name decides. A category is a path, not a product: ALDI files milk under
	// "Lácteos y huevos > Leche y bebidas vegetales", and reading that path as a domain made
	// every ALDI milk a plant drink. The category is only consulted when the name says
	// nothing, which is the case on MAS ("Redondo", "Tomate Frito").
	for _, text := range []string{fold(p.Name + " " + p.Brand), fold(p.Category)} {
		for _, r := range domainRules {
			if strings.Contains(text, r.word) {
				return r.name
			}
		}
	}
	return ""
}

func domainLabel(d string) string {
	if l, ok := domainLabels[d]; ok {
		return l
	}
	return d
}

// ---------- variants ----------

type axisValue struct {
	axis  string
	value string
	words []string
}

// axisTable is the typed vocabulary. An axis is a question with one answer, so "extra"
// (a grade) and "redondo" (a grain) can never contradict each other, which the first
// prototype got wrong: it read "arroz extra" against "arroz redondo" as different products.
// Words are matched on the folded text, longest first, and a product holds at most one
// value per axis.
var axisTable = []axisValue{
	{"line", "sin lactosa", []string{"sin lactosa"}},
	{"line", "sin gluten", []string{"sin gluten"}},
	{"line", "sin azucares", []string{"sin azucares", "0% azucares", "0 % azucares", "zero"}},
	{"line", "integral", []string{"integral"}},
	{"line", "eco", []string{"ecologico", "ecologica", "bio"}},
	{"line", "light", []string{"light", "0% 0%"}},
	{"line", "sin corteza", []string{"sin corteza"}},
	{"line", "brioche", []string{"brioche"}},
	{"line", "semillas", []string{"semillas", "pipas"}},
	{"line", "espelta", []string{"espelta"}},
	{"line", "centeno", []string{"centeno"}},
	{"fat", "entera", []string{"entera"}},
	{"fat", "semidesnatada", []string{"semidesnatada"}},
	{"fat", "desnatada", []string{"desnatada", "desnatado"}},
	{"grain", "basmati", []string{"basmati"}},
	{"grain", "bomba", []string{"bomba"}},
	{"grain", "vaporizado", []string{"vaporizado"}},
	{"grain", "redondo", []string{"redondo"}},
	{"grain", "largo", []string{"largo"}},
	{"process", "cocido", []string{"cocido"}},
	{"process", "precocido", []string{"precocido"}},
	{"process", "ultracongelado", []string{"ultracongelado"}},
	{"process", "congelado", []string{"congelado"}},
	{"process", "microondas", []string{"microondas"}},
	{"process", "polvo", []string{"polvo"}},
	{"process", "liquido", []string{"liquido"}},
	{"process", "capsulas", []string{"capsula"}},
	{"process", "tiras", []string{"tira"}},
	{"process", "pasteurizada", []string{"pasteurizada"}},
	{"process", "fresco", []string{"fresco", "fresca"}},
	{"flavor", "artesano", []string{"casero", "tradicional", "artesana", "artesano"}},
	{"flavor", "con aceite de oliva", []string{"aceite de oliva"}},
	{"flavor", "marsella", []string{"marsella"}},
	{"flavor", "colonia", []string{"colonia"}},
	{"flavor", "delicadas", []string{"delicada"}},
	{"flavor", "azul", []string{"azul"}},
	{"flavor", "doble rollo", []string{"doble rollo"}},
	{"grade", "extra", []string{"extra"}},
	{"grade", "primera", []string{"primera"}},
	{"kind", "lejia", []string{"lejia"}},
	{"species", "codorniz", []string{"codorniz"}},
	{"species", "pato", []string{"pato"}},
	{"farming", "suelo", []string{"suelo"}},
	{"farming", "campero", []string{"campero", "campera", "camperos", "camperas"}},
	{"farming", "jaula", []string{"jaula"}},
}

// strictAxes are the axes where a value on one side alone still means another product:
// "leche entera" is not "leche entera sin lactosa", and "tomate frito" is not "tomate frito
// estilo casero". For the rest — grain, grade, fat, farming — the absence of a value is
// silence, not disagreement.
var strictAxes = map[string]bool{"line": true, "flavor": true, "species": true, "kind": true}

var axisLabels = map[string]string{
	"line": "línea", "fat": "grasa", "grain": "grano", "process": "elaboración",
	"flavor": "estilo", "grade": "grado", "farming": "crianza", "sizeGrade": "tamaño",
	"species": "especie", "kind": "tipo",
}

var eggGrades = map[string]string{
	"pequeno": "S", "pequenos": "S", "mediano": "M", "medianos": "M",
	"grande": "L", "grandes": "L", "extragrande": "XL", "extragrandes": "XL",
	"supergrande": "XL", "supergrandes": "XL",
}

var eggLetter = regexp.MustCompile(`\b(?:clase\s+)?(xl|s|m|l)\b`)

func axes(p Product) map[string]string {
	text := fold(p.Name + " " + p.Category)
	out := map[string]string{}
	for _, av := range axisTable {
		if _, taken := out[av.axis]; taken {
			continue
		}
		for _, w := range av.words {
			if strings.Contains(text, w) {
				out[av.axis] = av.value
				break
			}
		}
	}
	if Domain(p) == "huevos" {
		if _, taken := out["sizeGrade"]; !taken {
			for word, grade := range eggGrades {
				if strings.Contains(text, word) {
					out["sizeGrade"] = grade
					break
				}
			}
		}
		if _, taken := out["sizeGrade"]; !taken {
			if m := eggLetter.FindStringSubmatch(text); m != nil {
				out["sizeGrade"] = strings.ToUpper(m[1])
			}
		}
	}
	return out
}

// axisDiff returns the first axis on which the candidate contradicts the anchor, or "".
func axisDiff(anchor, cand map[string]string) (axis, value string) {
	names := make([]string, 0, len(anchor)+len(cand))
	for a := range anchor {
		names = append(names, a)
	}
	for a := range cand {
		if _, ok := anchor[a]; !ok {
			names = append(names, a)
		}
	}
	sort.Strings(names)
	for _, a := range names {
		av, aok := anchor[a]
		cv, cok := cand[a]
		switch {
		case aok && cok && av != cv:
			return a, cv
		case !aok && cok && strictAxes[a]:
			return a, cv
		case aok && !cok && strictAxes[a]:
			return a, av
		}
	}
	return "", ""
}

// ---------- sizes ----------

// Size is a sold size in one base unit: grams, millilitres or units, times how many
// packets. Chains print the same size six ways ("6 x 1 L", "6 l", "p6x1l", "1k", "820gr"),
// so sizes are compared, never strings.
type Size struct {
	Dim   string
	Qty   float64
	Count int
}

// Total is the size in base units across the pack.
func (s Size) Total() float64 { return s.Qty * float64(s.Count) }

// String prints a size the way the UI shows it.
func (s Size) String() string {
	t := s.Total()
	switch s.Dim {
	case "g", "ml":
		if t >= 1000 {
			unit := s.Dim
			if unit == "g" {
				unit = "kg"
			} else {
				unit = "L"
			}
			return trimNum(t/1000) + " " + unit
		}
		return trimNum(t) + " " + s.Dim
	default:
		return trimNum(t) + " ud"
	}
}

func trimNum(f float64) string {
	s := fmt.Sprintf("%.2f", f)
	s = strings.TrimRight(s, "0")
	s = strings.TrimSuffix(s, ".")
	return strings.Replace(s, ".", ",", 1)
}

var sizeUnits = map[string]struct {
	dim string
	mul float64
}{
	"kg": {"g", 1000}, "kilo": {"g", 1000}, "kilos": {"g", 1000},
	"g": {"g", 1}, "gr": {"g", 1}, "grs": {"g", 1}, "gramo": {"g", 1}, "gramos": {"g", 1},
	"mg": {"g", 0.001},
	"l":  {"ml", 1000}, "litro": {"ml", 1000}, "litros": {"ml", 1000},
	"ml": {"ml", 1}, "cl": {"ml", 10},
	"ud": {"ud", 1}, "uds": {"ud", 1}, "unidad": {"ud", 1}, "unidades": {"ud", 1},
	"u": {"ud", 1}, "rollo": {"ud", 1}, "rollos": {"ud", 1},
}

var (
	sizePack   = regexp.MustCompile(`(?:^|\s)p?(\d+)\s*x\s*(\d+(?:[.,]\d+)?)\s*(kg|kilos?|grs?|gramos?|g|mg|ml|cl|litros?|l|uds?|unidades?|rollos?)\b`)
	sizeSingle = regexp.MustCompile(`(\d+(?:[.,]\d+)?)\s*(kg|kilos?|grs?|gramos?|g|mg|ml|cl|litros?|l|uds?|unidades?|rollos?)\b`)
	sizeKilo   = regexp.MustCompile(`(\d+(?:[.,]\d+)?)\s*k\b`)
)

func num(s string) float64 {
	var f float64
	fmt.Sscanf(strings.Replace(s, ",", ".", 1), "%f", &f)
	return f
}

// ParseSize reads the size a product publishes: the chain's own field first, then the
// name, which is where DIA, ALDI and MAS put it.
func ParseSize(p Product) *Size {
	if s := parseSizeText(fold(p.Size)); s != nil {
		return s
	}
	return parseSizeText(fold(p.Name))
}

func parseSizeText(t string) *Size {
	if t == "" {
		return nil
	}
	if m := sizePack.FindStringSubmatch(t); m != nil {
		u, ok := sizeUnits[m[3]]
		if !ok {
			return nil
		}
		return &Size{Dim: u.dim, Qty: num(m[2]) * u.mul, Count: atoi(m[1])}
	}
	if m := sizeSingle.FindStringSubmatch(t); m != nil {
		u, ok := sizeUnits[m[2]]
		if !ok {
			return nil
		}
		return &Size{Dim: u.dim, Qty: num(m[1]) * u.mul, Count: 1}
	}
	if m := sizeKilo.FindStringSubmatch(t); m != nil {
		return &Size{Dim: "g", Qty: num(m[1]) * 1000, Count: 1}
	}
	return nil
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// sameSize compares two published sizes. Six percent is the slack between a chain that
// prints 820 g and one that prints 0.82 kg; anything wider is another format.
func sameSize(a, b *Size) bool {
	if a == nil || b == nil || a.Dim != b.Dim {
		return false
	}
	ta, tb := a.Total(), b.Total()
	if ta <= 0 || tb <= 0 {
		return false
	}
	d := ta - tb
	if d < 0 {
		d = -d
	}
	max := ta
	if tb > max {
		max = tb
	}
	return d/max <= 0.06
}

// ---------- the judgement ----------

// CoversQuery reports whether a candidate is the thing the shopper asked for: it covers at
// least 60 % of the query's words (the threshold multisuper's basket already matches at)
// and the query's head noun appears among the candidate's first three words after its
// brand. The head rule is what keeps "compacto para hasta 3 huevos" from being eggs.
func CoversQuery(query string, p Product) bool {
	q := tokens(query)
	if len(q) == 0 {
		return true
	}
	brandTokens := tokenSet(p.Brand)
	name := make([]string, 0, 8)
	for _, t := range tokens(p.Name) {
		if !brandTokens[t] {
			name = append(name, t)
		}
	}
	if len(name) == 0 {
		return false
	}
	head := name
	if len(head) > 3 {
		head = head[:3]
	}
	found := false
	for _, t := range head {
		if t == q[0] {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	nameSet := make(map[string]bool, len(name))
	for _, t := range name {
		nameSet[t] = true
	}
	hit := 0
	for _, t := range q {
		if nameSet[t] {
			hit++
		}
	}
	return float64(hit)/float64(len(q)) >= 0.6
}

// Compare judges one candidate against the anchor of a list item.
func Compare(query string, anchor, cand Product) Match {
	m := Match{Product: cand, Level: LevelNo}

	if anchor.EAN != "" && cand.EAN != "" && anchor.EAN == cand.EAN {
		m.Level, m.Score, m.Reason = LevelExact, 100, "mismo código de barras"
		return m
	}

	da, dc := Domain(anchor), Domain(cand)
	if da != "" && dc != "" && da != dc {
		m.Reason = "otro tipo de producto: " + domainLabel(dc)
		return m
	}

	if !CoversQuery(query, cand) {
		m.Reason = "no es el producto que pediste"
		return m
	}

	reasons := make([]string, 0, 3)
	if axis, value := axisDiff(axes(anchor), axes(cand)); axis != "" {
		m.Level = LevelMaybe
		m.Score = 30
		reasons = append(reasons, fmt.Sprintf("otra variante (%s: %s)", axisLabels[axis], value))
	} else if as, cs := ParseSize(anchor), ParseSize(cand); sameSize(as, cs) {
		m.Level = LevelSame
		m.Score = 60
	} else {
		m.Level = LevelMaybe
		m.Score = 30
		switch {
		case as == nil || cs == nil:
			reasons = append(reasons, "sin tamaño publicado")
		default:
			reasons = append(reasons, fmt.Sprintf("otro formato (%s → %s)", as, cs))
		}
	}

	if ao, co := OwnBrand(anchor), OwnBrand(cand); ao != nil && co != nil {
		if *ao == *co {
			m.Score += 10
			if *ao {
				reasons = append(reasons, "marca blanca de la cadena")
			}
		} else {
			m.Score -= 15
			reasons = append(reasons, "marca distinta")
		}
	}
	m.Score += int(overlap(anchor.Name, cand.Name) * 8)
	m.Reason = strings.Join(reasons, "; ")
	return m
}

// overlap is the share of the anchor's content words that the candidate also prints. It is
// the tie-breaker between two candidates that are equally probable: a cheaper bleach that
// shares one word loses to a detergent that shares two.
func overlap(anchorName, candName string) float64 {
	a, c := tokenSet(anchorName), tokenSet(candName)
	if len(a) == 0 {
		return 0
	}
	hit := 0
	for t := range a {
		if c[t] {
			hit++
		}
	}
	return float64(hit) / float64(len(a))
}

// Best judges every candidate and returns the best one that is not `no`, plus the ranked
// list of the alternatives (also without `no`). The caller shows the alternatives so a
// shopper can overrule the machine, which is the only honest way to use a heuristic.
func Best(query string, anchor Product, cands []Product) (Match, []Match) {
	matches := make([]Match, 0, len(cands))
	for _, c := range cands {
		if m := Compare(query, anchor, c); m.Level != LevelNo {
			matches = append(matches, m)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		pa, pb := priceOf(a.Product), priceOf(b.Product)
		if pa != pb {
			return pa < pb
		}
		if a.Product.Name != b.Product.Name {
			return a.Product.Name < b.Product.Name
		}
		return a.Product.ID < b.Product.ID
	})
	if len(matches) == 0 {
		return Match{Level: LevelNo, Reason: "no encontrado"}, nil
	}
	return matches[0], matches[1:]
}

// rank is a chain's place in the app's own order, so a tie on price has one deterministic
// answer instead of an alphabetical one ("alcampo" before "dia" made Alcampo the base).
func rank(storeName string, order []string) int {
	for i, s := range order {
		if s == storeName {
			return i
		}
	}
	return len(order)
}

func priceOf(p Product) int64 {
	if p.PriceCents == nil {
		return 1 << 62
	}
	return *p.PriceCents
}

// PickAnchor chooses the anchor of a fresh item: the product the shopper would have added
// first. Among the candidates that cover the query it prefers the chain's own brand and
// the size the other chains also sell, then the cheapest — the same reasoning the measured
// pass followed when DIA, Mercadona and ALDI priced 1 L of own-brand whole milk at 0,96 €.
func PickAnchor(query string, cands []Product, order []string) (Product, bool) {
	covering := make([]Product, 0, len(cands))
	for _, c := range cands {
		if c.PriceCents != nil && CoversQuery(query, c) {
			covering = append(covering, c)
		}
	}
	if len(covering) == 0 {
		return Product{}, false
	}
	own := make([]Product, 0, len(covering))
	for _, c := range covering {
		if ob := OwnBrand(c); ob != nil && *ob {
			own = append(own, c)
		}
	}
	pool := covering
	if len(own) > 0 {
		pool = own
	}
	modal := map[string]int{}
	for _, c := range pool {
		if s := ParseSize(c); s != nil {
			modal[s.String()]++
		}
	}
	best, top := "", 0
	for label, n := range modal {
		if best == "" || n > top || (n == top && label < best) {
			best, top = label, n
		}
	}
	// A mode of one is not a mode: with tomato sauce every chain sells another jar size, and
	// filtering by the alphabetically first label picked the wrong base.
	if best != "" && top >= 2 {
		atSize := make([]Product, 0, len(pool))
		for _, c := range pool {
			if s := ParseSize(c); s != nil && s.String() == best {
				atSize = append(atSize, c)
			}
		}
		if len(atSize) > 0 {
			pool = atSize
		}
	}
	// The plainest product of that size wins: the shopper who types "leche entera" means
	// the plain litre, not the cheaper lactose-free one sitting beside it.
	sort.SliceStable(pool, func(i, j int) bool {
		ai, aj := len(axes(pool[i])), len(axes(pool[j]))
		if ai != aj {
			return ai < aj
		}
		if priceOf(pool[i]) != priceOf(pool[j]) {
			return priceOf(pool[i]) < priceOf(pool[j])
		}
		return rank(pool[i].Store, order) < rank(pool[j].Store, order)
	})
	return pool[0], true
}
