package identity

// The cases here are the real product names and prices measured on 2026-11-08 with
// `multisuper find` over the seven chains. They are the contract of this package: the four
// false positives that the first prototype produced (`cuecehuevos`, `escobilla`, `anchoas en
// aceite de girasol`, `molde de silicona`) and the +74 % fake price of "tomate frito casero"
// stay in the suite as negative cases forever.

import "testing"

func p(store, name, brand, size string, cents int64) Product {
	c := cents
	return Product{Store: store, ID: store + ":" + name, Name: name, Brand: brand, Size: size, PriceCents: &c}
}

func TestOwnBrand(t *testing.T) {
	cases := []struct {
		name  string
		p     Product
		want  bool
		known bool
	}{
		{"dia sub-brand", p("dia", "Leche entera Dia Láctea 1 L", "Dia Láctea", "", 96), true, true},
		{"dia prefix", p("dia", "Aceite refinado de girasol Diasol 1 L", "Diasol", "", 185), true, true},
		{"dia mill", p("dia", "Pan de molde blanco El Molino de Dia 820 g", "El Molino de Dia", "", 115), true, true},
		{"dia manufacturer", p("dia", "Leche entera Asturiana 1 L", "Asturiana", "", 129), false, true},
		{"mas ifa in name", p("mas", "Leche entera IFA ELIGES brick 1l", "", "", 96), true, true},
		{"mas ifa smashed", p("mas", "Horchata ifaeliges1l", "", "", 115), true, true},
		{"mas manufacturer, no field", p("mas", "Leche entera ACORES brick 1l", "", "", 95), false, false},
		{"mercadona", p("mercadona", "Leche entera Hacendado", "Hacendado", "1 l", 96), true, true},
		{"aldi trademark", p("aldi", "Leche entera", "MILSANI®", "", 96), true, true},
		{"carrefour", p("carrefour", "Pan de molde sin corteza Carrefour Classic 820 g", "Blanco sin corteza maxi", "", 188), true, true},
	}
	for _, c := range cases {
		got := OwnBrand(c.p)
		if got == nil {
			if c.known {
				t.Errorf("%s: got unknown, want %v", c.name, c.want)
			}
			continue
		}
		if !c.known {
			t.Errorf("%s: got %v, want unknown", c.name, *got)
			continue
		}
		if *got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, *got, c.want)
		}
	}
}

func TestParseSize(t *testing.T) {
	cases := []struct {
		p    Product
		want string
	}{
		{p("dia", "Leche entera Dia Láctea pack 6 x 1 L", "", "", 576), "6 L"},
		{p("mercadona", "Leche entera Hacendado", "", "6 l", 576), "6 L"},
		{p("mas", "Galleta maria  IFA ELIGES p4x200g", "", "", 135), "800 g"},
		{p("mas", "Arroz largo  NOMEN  1k", "", "", 155), "1 kg"},
		{p("dia", "Pan de molde blanco El Molino de Dia 820 g", "", "", 115), "820 g"},
		{p("mercadona", "Pan de molde blanco familiar Hacendado", "", "0.82 kg", 115), "820 g"},
		{p("mas", "Huevos clase m CASA DE CASTRELO 12ud", "", "", 285), "12 ud"},
		{p("dia", "Huevos medianos M Dia 24 unidades", "", "", 525), "24 ud"},
		{p("mercadona", "Tomate frito Hacendado", "", "1.2 kg", 135), "1,2 kg"},
	}
	for _, c := range cases {
		if got := ParseSize(c.p); got == nil || got.String() != c.want {
			t.Errorf("%q: got %v, want %s", c.p.Name, got, c.want)
		}
	}
	if got := ParseSize(p("dia", "Detergente líquido para prendas delicadas Dia Super Paco 66 lavados", "Dia Super Paco", "", 250)); got != nil {
		t.Errorf("a wash count is not a size: got %v", got)
	}
}

func TestDomain(t *testing.T) {
	cases := map[string]string{
		"Leche entera Dia Láctea 1 L":                                  "lacteos",
		"Filetes de anchoa en aceite de girasol Dia Mari Marinera":     "conservas",
		"Aceite refinado de girasol Diasol 1 L":                        "aceite" + "s",
		"SILVERCREST® Cuecehuevos compacto para hasta 3 huevos":        "hogar",
		"LIVARNO HOME Escobobilla de váter / Caja de pañuelos":         "hogar",
		"SILVERCREST® Juego de moldes de silicona para barritas / pan": "hogar",
		"Pan de molde blanco El Molino de Dia 820 g":                   "pan",
		"Bebida de almendras":                                          "bebidas",
		"MAX & MILA'S WORLD Croquetas de pollo y arroz para perro":     "mascotas",
		"Huevos de chocolate rellenos de crema de leche con avellanas": "dulces",
		"Galletas María Dorada":                                        "galletas",
		// The category is a path, not a product: ALDI files milk under a section that names
		// plant drinks, and reading that as the domain made every ALDI milk a plant drink.
		"Leche entera": "lacteos",
	}
	for name, want := range cases {
		if got := Domain(Product{Name: name}); got != want {
			t.Errorf("%q: got %q, want %q", name, got, want)
		}
	}
}

func TestCompareMeasuredCorpus(t *testing.T) {
	leche := p("dia", "Leche entera Dia Láctea 1 L", "Dia Láctea", "", 96)
	huevos := p("dia", "Huevos medianos M de gallinas criadas en suelo Dia 12 unidades", "Dia", "", 285)
	aceite := p("dia", "Aceite refinado de girasol Diasol 1 L", "Diasol", "", 185)
	pan := p("dia", "Pan de molde blanco El Molino de Dia 820 g", "El Molino de Dia", "", 115)
	tomate := p("dia", "Tomate frito 0% azúcares añadidos Dia Vegecampo 390 g", "Dia Vegecampo", "", 69)
	arroz := p("dia", "Arroz extra Dia Arrozona 1 Kg", "Dia Arrozona", "", 115)
	galletas := p("dia", "Galletas maría dorada Dia Galleteca 800 g", "Dia Galleteca", "", 135)
	papel := p("dia", "Papel higiénico Dia La Llama 12 unidades", "Dia La Llama", "", 235)

	cases := []struct {
		name, query string
		anchor      Product
		cand        Product
		want        string
	}{
		{"milk same size", "leche entera", leche,
			p("mercadona", "Leche entera Hacendado", "Hacendado", "1 l", 96), LevelSame},
		{"milk manufacturer same size", "leche entera", leche,
			p("mas", "Leche entera ACORES brick 1l", "", "", 95), LevelSame},
		{"lactose free is another product", "leche entera", leche,
			p("mercadona", "Leche entera sin lactosa Hacendado", "Hacendado", "1 l", 94), LevelMaybe},
		{"plant drink is not milk", "leche entera", leche,
			p("aldi", "Bebida de almendras", "MYVAY®", "", 105), LevelNo},
		{"eggs same grade and dozen", "huevos", huevos,
			p("mas", "Huevos clase m CASA DE CASTRELO 12ud", "", "", 285), LevelSame},
		{"eggs same dozen, other grade", "huevos", huevos,
			p("dia", "Huevos grandes L Dia 18 unidades", "Dia", "", 439), LevelMaybe},
		{"eggs free range", "huevos", huevos,
			p("mas", "Huevos camperos SAN RAFAEL 12ud", "", "", 335), LevelMaybe},
		{"eggs of another species", "huevos", huevos,
			p("mas", "Huevos frescos codorniz  SAN RAFAEL  12ud", "", "", 185), LevelMaybe},
		{"NEGATIVE: egg cooker", "huevos", huevos,
			p("lidl", "SILVERCREST® Cuecehuevos compacto para hasta 3 huevos", "SILVERCREST®", "", 899), LevelNo},
		{"NEGATIVE: chocolate eggs", "huevos", huevos,
			p("aldi", "Huevos de chocolate rellenos de crema de leche con avellanas", "CHOCEUR®", "", 299), LevelNo},
		{"bleach is not laundry detergent", "detergente",
			p("mercadona", "Detergente ropa Frescura Bosque Verde líquido", "Bosque Verde", "3 l", 350),
			p("dia", "Lejía con detergente Dia 2 L", "Dia", "", 115), LevelMaybe},
		{"NEGATIVE: toilet brush", "papel higienico", papel,
			p("lidl", "LIVARNO HOME Escobobilla de váter / Caja de pañuelos", "LIVARNO HOME", "", 499), LevelNo},
		{"NEGATIVE: anchovies in sunflower oil", "aceite de girasol", aceite,
			p("dia", "Filetes de anchoa en aceite de girasol Dia Mari Marinera 3 x 25 g", "Dia Mari Marinera", "", 279), LevelNo},
		{"NEGATIVE: silicone bread mould", "pan de molde", pan,
			p("lidl", "SILVERCREST® Juego de moldes de silicona para barritas / pan", "SILVERCREST®", "", 699), LevelNo},
		{"bread same format", "pan de molde", pan,
			p("mercadona", "Pan de molde blanco familiar Hacendado", "Hacendado", "0.82 kg", 115), LevelSame},
		{"bread other format", "pan de molde", pan,
			p("aldia", "Pan de molde blanco", "EL HORNO®", "", 109), LevelMaybe},
		{"NEGATIVE: +74% is another recipe", "tomate frito", tomate,
			p("mercadona", "Tomate frito estilo casero Hacendado", "Hacendado", "0.38 kg", 120), LevelMaybe},
		{"tomato other format", "tomate frito", tomate,
			p("mercadona", "Tomate frito Hacendado", "Hacendado", "0.56 kg", 90), LevelMaybe},
		{"rice other grain, size unpublished", "arroz", arroz,
			p("aldi", "Arroz redondo", "BON-RI®", "", 109), LevelMaybe},
		{"rice same grain (anchor grain silent)", "arroz", arroz,
			p("mercadona", "Arroz redondo Hacendado", "Hacendado", "1 kg", 115), LevelSame},
		{"biscuits same size", "galletas maria", galletas,
			p("mercadona", "Galletas María dorada Hacendado", "Hacendado", "0.8 kg", 135), LevelSame},
		{"biscuits size unpublished", "galletas maria", galletas,
			p("aldi", "Galletas María Dorada", "BISCOTTO®", "", 129), LevelMaybe},
	}
	for _, c := range cases {
		if got := Compare(c.query, c.anchor, c.cand).Level; got != c.want {
			t.Errorf("%s: got %s, want %s (%s)", c.name, got, c.want,
				Compare(c.query, c.anchor, c.cand).Reason)
		}
	}
}

func TestCompareEAN(t *testing.T) {
	a := p("dia", "Leche entera 1 L", "Dia", "", 96)
	a.EAN = "8410297110979"
	b := p("carrefour", "Leche entera Central Lechera Asturiana botella 2,2 l.", "central lechera asturiana", "2200 ml", 275)
	b.EAN = "8410297110979"
	if got := Compare("leche entera", a, b).Level; got != LevelExact {
		t.Errorf("same barcode: got %s, want %s", got, LevelExact)
	}
}

func TestPickAnchorPrefersOwnBrandAndModalSize(t *testing.T) {
	cands := []Product{
		p("dia", "Leche entera Dia Láctea 6 x 1 L", "Dia Láctea", "", 576),
		p("dia", "Leche entera Dia Láctea 1 L", "Dia Láctea", "", 96),
		p("mercadona", "Leche entera Hacendado", "Hacendado", "1 l", 96),
		p("aldi", "Leche entera", "MILSANI®", "", 96),
		p("mas", "Leche entera ACORES brick 1l", "", "", 95),
		p("mercadona", "Leche entera sin lactosa Hacendado", "Hacendado", "1 l", 94),
	}
	got, ok := PickAnchor("leche entera", cands, []string{"dia", "mercadona", "mas", "aldi", "lidl", "carrefour", "alcampo"})
	if !ok {
		t.Fatal("no anchor")
	}
	if got.Store != "dia" || got.Name != "Leche entera Dia Láctea 1 L" {
		t.Errorf("got %s/%s, want the DIA own-brand litre", got.Store, got.Name)
	}
}

func TestCategoryPathIsNotADomain(t *testing.T) {
	anchor := p("dia", "Leche entera Dia Láctea 1 L", "Dia Láctea", "", 96)
	anchor.Category = "Leche"
	aldi := p("aldi", "Leche entera", "MILSANI®", "", 96)
	aldi.Category = "Lácteos y huevos > Leche y bebidas vegetales"
	if got := Domain(aldi); got != "lacteos" {
		t.Fatalf("domain: got %q, want lacteos", got)
	}
	if got := Compare("leche entera", anchor, aldi).Level; got != LevelMaybe {
		t.Errorf("ALDI milk: got %s, want probable because it publishes no size", got)
	}
}

func TestQueryDomainNamesWhatTheShopperAskedFor(t *testing.T) {
	for query, want := range map[string]string{
		"huevos": "huevos", "leche entera": "lacteos", "aceite de girasol": "aceites",
		"detergente": "drogueria", "pan de molde": "pan", "tomate frito": "salsas",
	} {
		if got := QueryDomain(query); got != want {
			t.Errorf("QueryDomain(%q) = %q, want %q", query, got, want)
		}
	}
}

func TestPickAnchorRejectsAnotherDomain(t *testing.T) {
	choc := p("aldi", "Huevos de chocolate rellenos de crema de leche", "CHOCEUR®", "", 299)
	plain := p("mas", "Huevos frescos clase l SAN RAFAEL 6ud", "", "", 180)
	got, ok := PickAnchor("huevos", []Product{choc, plain}, nil)
	if !ok || got.Name != plain.Name {
		t.Fatalf("got %+v, want the plain eggs", got)
	}
	if _, ok := PickAnchor("huevos", []Product{choc}, nil); ok {
		t.Error("chocolate eggs alone cannot be the base of a list about eggs")
	}
}

func TestPickAnchorIgnoresAModeOfOne(t *testing.T) {
	cands := []Product{
		p("dia", "Tomate frito Dia Vegecampo 550 g", "Dia Vegecampo", "", 89),
		p("mercadona", "Tomate frito Hacendado", "Hacendado", "0.56 kg", 90),
		p("mas", "Tomate frito IFA ELIGES frasco 570gr", "", "", 90),
		p("mercadona", "Tomate frito Hacendado", "Hacendado", "1.2 kg", 135),
	}
	got, ok := PickAnchor("tomate frito", cands, nil)
	if !ok || got.Name != "Tomate frito Dia Vegecampo 550 g" {
		t.Errorf("got %+v, want the cheapest plain jar", got)
	}
}

func TestBestRanksOwnBrandFirst(t *testing.T) {
	anchor := p("dia", "Leche entera Dia Láctea 1 L", "Dia Láctea", "", 96)
	own := p("mercadona", "Leche entera Hacendado", "Hacendado", "1 l", 96)
	other := p("mas", "Leche entera PASCUAL brick 1l", "", "", 96)
	best, rest := Best("leche entera", anchor, []Product{other, own})
	if best.Product.Brand != "Hacendado" {
		t.Errorf("best: got %q, want Hacendado", best.Product.Brand)
	}
	if len(rest) != 1 || rest[0].Product.Brand != "" {
		t.Errorf("rest: got %v", rest)
	}
}

func TestCoversQueryHeadRule(t *testing.T) {
	if CoversQuery("huevos", Product{Name: "SILVERCREST® Cuecehuevos compacto para hasta 3 huevos", Brand: "SILVERCREST®"}) {
		t.Error("the head noun must appear in the first words, not in the tail")
	}
	if !CoversQuery("huevos", Product{Name: "Huevos clase m CASA DE CASTRELO 12ud"}) {
		t.Error("a dozen eggs must cover the query")
	}
}
