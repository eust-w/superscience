package science_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"superscience/internal/science"
)

func TestRegistryListsCoreDatabases(t *testing.T) {
	registry := science.NewRegistry(nil)
	ids := map[string]bool{}
	for _, db := range registry.Databases() {
		ids[db.ID] = true
	}
	for _, want := range []string{
		"pubmed",
		"arxiv",
		"uniprot",
		"pubchem",
		"openalex",
		"semantic_scholar",
		"crossref",
		"europe_pmc",
		"pdb",
		"alphafold",
		"chembl",
		"ensembl",
		"clinicaltrials",
		"ncbi_gene",
		"ncbi_protein",
		"bindingdb",
		"chebi",
		"gtopdb",
		"surechembl",
		"clinvar",
		"dbsnp",
		"gnomad",
		"mygene",
		"myvariant",
		"ucsc",
		"geo",
		"gtex",
		"hpa",
		"depmap",
		"expression_atlas",
		"single_cell_atlas",
		"biogrid",
		"intact",
		"kegg",
		"opentargets",
		"reactome",
		"string",
		"wikipathways",
		"interpro",
		"pdbe",
		"sifts",
		"biorxiv",
		"datacite",
		"zenodo",
		"nasa_exoplanet",
		"inspire_hep",
		"nomad_materials",
		"hf_robotics_datasets",
		"github_robotics",
		"ros_packages",
		"openreview_embodied",
	} {
		if !ids[want] {
			t.Fatalf("missing database %q in %+v", want, ids)
		}
	}
}

func TestSearchUsesExpandedStructureChemistryAndClinicalEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pdb":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result_set": []map[string]any{{"identifier": "1ABC", "score": 0.91}},
			})
		case "/alphafold":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"entryId":                "AF-P00533-F1",
				"proteinDescription":     "EGFR predicted structure",
				"gene":                   "EGFR",
				"organismScientificName": "Homo sapiens",
				"uniprotAccession":       "P00533",
			}})
		case "/chembl":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"molecules": []map[string]any{{
					"molecule_chembl_id": "CHEMBL1",
					"pref_name":          "Gefitinib",
					"molecule_type":      "Small molecule",
				}},
			})
		case "/clinical":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"studies": []map[string]any{{
					"protocolSection": map[string]any{
						"identificationModule": map[string]any{"nctId": "NCT1", "briefTitle": "EGFR trial"},
						"statusModule":         map[string]any{"overallStatus": "RECRUITING"},
					},
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	registry := science.NewRegistryWithEndpoints(server.Client(), science.Endpoints{
		PDB:            server.URL + "/pdb",
		AlphaFold:      server.URL + "/alphafold",
		ChEMBL:         server.URL + "/chembl",
		ClinicalTrials: server.URL + "/clinical",
	})
	for _, tc := range []struct {
		database string
		title    string
	}{
		{"pdb", "PDB structure 1ABC"},
		{"alphafold", "EGFR predicted structure"},
		{"chembl", "Gefitinib"},
		{"clinicaltrials", "EGFR trial"},
	} {
		hits, err := registry.Search(context.Background(), science.SearchRequest{Database: tc.database, Query: "EGFR", Limit: 2})
		if err != nil {
			t.Fatalf("search %s: %v", tc.database, err)
		}
		if len(hits) != 1 || hits[0].Title != tc.title || hits[0].Provenance.URL == "" {
			t.Fatalf("%s hits = %+v", tc.database, hits)
		}
	}
}

func TestSearchUsesLiveGenomicsPathwayNetworkAndPreprintEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ensembl/xrefs/symbol/homo_sapiens/EGFR":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"id":          "ENSG00000146648",
				"display_id":  "EGFR",
				"description": "epidermal growth factor receptor",
				"type":        "gene",
				"dbname":      "HGNC",
			}})
		case "/clinvar/esearch.fcgi":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"esearchresult": map[string]any{"idlist": []string{"12345"}},
			})
		case "/clinvar/esummary.fcgi":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"uids": []string{"12345"},
					"12345": map[string]any{
						"title":                 "NM_005228.5(EGFR):c.2369C>T",
						"clinical_significance": "Pathogenic",
						"variation_name":        "EGFR variant",
						"accession":             "VCV000012345",
					},
				},
			})
		case "/mygene/v3/query":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"hits": []map[string]any{{
					"_id":        "1956",
					"symbol":     "EGFR",
					"name":       "epidermal growth factor receptor",
					"taxid":      9606,
					"entrezgene": "1956",
				}},
			})
		case "/reactome/ContentService/search/query":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{
					"stId":        "R-HSA-177929",
					"name":        "Signaling by EGFR",
					"summation":   "EGFR pathway",
					"speciesName": "Homo sapiens",
					"type":        "Pathway",
				}},
			})
		case "/string/api/json/interaction_partners":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"stringId_A":        "9606.ENSP00000275493",
				"preferredName_A":   "EGFR",
				"stringId_B":        "9606.ENSP00000354587",
				"preferredName_B":   "GRB2",
				"annotation":        "Known EGFR interaction partner",
				"score":             0.982,
				"experimental":      0.81,
				"database":          0.75,
				"coexpression":      0.32,
				"neighborhood":      0.12,
				"fusion":            0.02,
				"cooccurence":       0.01,
				"textmining":        0.69,
				"homology":          0,
				"ncbiTaxonId":       9606,
				"preferredName_A_1": "ignored by parser",
			}})
		case "/biorxiv/details/biorxiv/30d/0/json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"collection": []map[string]any{{
					"doi":      "10.1101/2026.01.01.123456",
					"title":    "EGFR preprint",
					"authors":  "A. Author; B. Writer",
					"abstract": "EGFR preprint abstract",
					"date":     "2026-01-02",
					"category": "Cancer Biology",
					"server":   "biorxiv",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	registry := science.NewRegistryWithEndpoints(server.Client(), science.Endpoints{
		Ensembl:  server.URL + "/ensembl",
		ClinVar:  server.URL + "/clinvar",
		MyGene:   server.URL + "/mygene/v3/query",
		Reactome: server.URL + "/reactome/ContentService",
		STRING:   server.URL + "/string/api/json/interaction_partners",
		BioRxiv:  server.URL + "/biorxiv/details/biorxiv",
	})
	for _, tc := range []struct {
		database string
		title    string
	}{
		{"ensembl", "EGFR"},
		{"clinvar", "NM_005228.5(EGFR):c.2369C>T"},
		{"mygene", "EGFR"},
		{"reactome", "Signaling by EGFR"},
		{"string", "EGFR-GRB2"},
		{"biorxiv", "EGFR preprint"},
	} {
		hits, err := registry.Search(context.Background(), science.SearchRequest{Database: tc.database, Query: "EGFR", Limit: 2})
		if err != nil {
			t.Fatalf("search %s: %v", tc.database, err)
		}
		if len(hits) != 1 || hits[0].Title != tc.title || hits[0].Metadata["database"] != tc.database || hits[0].Provenance.URL == "" {
			t.Fatalf("%s hits = %+v", tc.database, hits)
		}
	}
}

func TestSearchUsesLiveNCBIGeneAndProteinEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gene/esearch.fcgi":
			if got := r.URL.Query().Get("db"); got != "gene" {
				t.Fatalf("gene esearch db = %s", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"esearchresult": map[string]any{"idlist": []string{"1956"}},
			})
		case "/gene/esummary.fcgi":
			if got := r.URL.Query().Get("db"); got != "gene" {
				t.Fatalf("gene esummary db = %s", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"uids": []string{"1956"},
					"1956": map[string]any{
						"uid":                "1956",
						"name":               "EGFR",
						"description":        "epidermal growth factor receptor",
						"chromosome":         "7",
						"maplocation":        "7p11.2",
						"summary":            "A receptor tyrosine kinase.",
						"otheraliases":       "ERBB, ERBB1, HER1",
						"nomenclaturesymbol": "EGFR",
					},
				},
			})
		case "/protein/esearch.fcgi":
			if got := r.URL.Query().Get("db"); got != "protein" {
				t.Fatalf("protein esearch db = %s", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"esearchresult": map[string]any{"idlist": []string{"2811086"}},
			})
		case "/protein/esummary.fcgi":
			if got := r.URL.Query().Get("db"); got != "protein" {
				t.Fatalf("protein esummary db = %s", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"uids": []string{"2811086"},
					"2811086": map[string]any{
						"uid":              "2811086",
						"caption":          "P00533",
						"title":            "RecName: Full=Epidermal growth factor receptor",
						"accessionversion": "P00533.2",
						"organism":         "Homo sapiens",
						"slen":             1210,
						"sourcedb":         "swiss_prot",
						"moltype":          "aa",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	registry := science.NewRegistryWithEndpoints(server.Client(), science.Endpoints{
		NCBIGene:    server.URL + "/gene",
		NCBIProtein: server.URL + "/protein",
	})
	for _, tc := range []struct {
		database string
		query    string
		title    string
	}{
		{"ncbi_gene", "EGFR", "EGFR"},
		{"ncbi_protein", "P00533", "P00533"},
	} {
		hits, err := registry.Search(context.Background(), science.SearchRequest{Database: tc.database, Query: tc.query, Limit: 2})
		if err != nil {
			t.Fatalf("search %s: %v", tc.database, err)
		}
		if len(hits) != 1 || hits[0].Title != tc.title || hits[0].Metadata["database"] != tc.database || hits[0].Provenance.URL == "" {
			t.Fatalf("%s hits = %+v", tc.database, hits)
		}
	}
}

func TestSearchUsesLiveCrossDisciplinaryEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/datacite/dois":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{
					"id": "10.5281/zenodo.1",
					"attributes": map[string]any{
						"doi":                 "10.5281/zenodo.1",
						"titles":              []map[string]any{{"title": "Climate model benchmark dataset"}},
						"publisher":           "Zenodo",
						"publicationYear":     2026,
						"resourceTypeGeneral": "Dataset",
						"creators": []map[string]any{{
							"name": "A. Researcher",
						}},
						"descriptions": []map[string]any{{
							"description": "Benchmark dataset for climate models.",
						}},
					},
				}},
			})
		case "/zenodo/api/records":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"hits": map[string]any{
					"hits": []map[string]any{{
						"id":      16537543,
						"doi":     "10.5281/zenodo.16537543",
						"doi_url": "https://doi.org/10.5281/zenodo.16537543",
						"metadata": map[string]any{
							"title":            "Reusable climate notebook archive",
							"description":      "<p>Notebook and data package.</p>",
							"publication_date": "2026-01-02",
							"creators":         []map[string]any{{"name": "Notebook Team"}},
							"resource_type":    map[string]any{"title": "Dataset", "type": "dataset"},
							"access_right":     "open",
						},
					}},
				},
			})
		case "/exoplanet/TAP/sync":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"pl_name":         "Kepler-467 b",
				"hostname":        "Kepler-467",
				"disc_year":       2016,
				"discoverymethod": "Transit",
				"sy_dist":         466.265,
			}})
		case "/inspire/api/literature":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"hits": map[string]any{
					"hits": []map[string]any{{
						"id": "2181837",
						"metadata": map[string]any{
							"titles":           []map[string]any{{"title": "Higgs boson analysis"}},
							"publication_info": []map[string]any{{"year": 2026, "journal_title": "Phys. Rev. D"}},
							"authors":          []map[string]any{{"full_name": "Lu, Yi-Song"}},
							"citation_count":   42,
							"abstracts":        []map[string]any{{"value": "A high energy physics analysis."}},
							"dois":             []map[string]any{{"value": "10.1000/hep.1"}},
						},
					}},
				},
			})
		case "/nomad/api/v1/entries/query":
			if r.Method != http.MethodPost {
				t.Fatalf("nomad method = %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{
					"entry_id":           "entry-1",
					"upload_create_time": "2026-01-02T03:04:05Z",
					"results": map[string]any{
						"material": map[string]any{
							"chemical_formula_hill": "O48Sr16Ti16",
							"elements":              []string{"O", "Sr", "Ti"},
						},
						"method": map[string]any{"method_name": "DFT"},
					},
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	registry := science.NewRegistryWithEndpoints(server.Client(), science.Endpoints{
		DataCite:       server.URL + "/datacite/dois",
		Zenodo:         server.URL + "/zenodo/api/records",
		NASAExoplanet:  server.URL + "/exoplanet/TAP/sync",
		INSPIREHEP:     server.URL + "/inspire/api/literature",
		NOMADMaterials: server.URL + "/nomad/api/v1",
	})
	for _, tc := range []struct {
		database string
		query    string
		title    string
	}{
		{"datacite", "climate", "Climate model benchmark dataset"},
		{"zenodo", "climate", "Reusable climate notebook archive"},
		{"nasa_exoplanet", "Kepler", "Kepler-467 b"},
		{"inspire_hep", "higgs", "Higgs boson analysis"},
		{"nomad_materials", "Ti O", "O48Sr16Ti16"},
	} {
		hits, err := registry.Search(context.Background(), science.SearchRequest{Database: tc.database, Query: tc.query, Limit: 2})
		if err != nil {
			t.Fatalf("search %s: %v", tc.database, err)
		}
		if len(hits) != 1 || hits[0].Title != tc.title || hits[0].Metadata["database"] != tc.database || hits[0].Provenance.URL == "" {
			t.Fatalf("%s hits = %+v", tc.database, hits)
		}
	}
}

func TestSearchUsesLiveRoboticsAndEmbodiedAIEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hf/api/datasets":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"id":           "lerobot/pusht",
				"author":       "lerobot",
				"description":  "LeRobot PushT manipulation dataset.",
				"downloads":    12345,
				"likes":        321,
				"tags":         []string{"task_categories:robotics", "embodied-ai", "manipulation"},
				"lastModified": "2026-01-02T03:04:05Z",
			}})
		case "/github/search/repositories":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{{
					"id":               748713144,
					"full_name":        "huggingface/lerobot",
					"description":      "State-of-the-art AI for real-world robotics in PyTorch",
					"html_url":         "https://github.com/huggingface/lerobot",
					"stargazers_count": 25000,
					"forks_count":      3200,
					"language":         "Python",
					"topics":           []string{"robotics", "imitation-learning", "reinforcement-learning"},
				}},
			})
		case "/ros/rolling/distribution.yaml":
			w.Header().Set("Content-Type", "text/yaml")
			_, _ = w.Write([]byte(`repositories:
  nav2:
    release:
      packages:
      - nav2_bringup
      - nav2_core
      url: https://github.com/ros2-gbp/navigation2-release.git
    source:
      url: https://github.com/ros-navigation/navigation2.git
    status: maintained
  ros2_control:
    release:
      packages:
      - ros2_control
      url: https://github.com/ros2-gbp/ros2_control-release.git
    source:
      url: https://github.com/ros-controls/ros2_control.git
    status: developed
`))
		case "/openreview/notes/search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"notes": []map[string]any{{
					"id": "SYsmAZ8PHez",
					"content": map[string]any{
						"title":    "InteriorSim: A Photorealistic Simulator for Embodied AI",
						"authors":  []string{"Mike Roberts", "Vladlen Koltun"},
						"keywords": []string{"simulation environment", "embodied AI"},
						"TL;DR":    "InteriorSim is a photorealistic simulator for embodied AI in the home.",
						"abstract": "Interactive simulators are becoming powerful tools for training embodied agents.",
					},
					"invitation": "ICLR.cc/2023/Conference/-/Withdrawn_Submission",
					"forum":      "SYsmAZ8PHez",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	registry := science.NewRegistryWithEndpoints(server.Client(), science.Endpoints{
		HFRoboticsDatasets: server.URL + "/hf/api/datasets",
		GitHubRobotics:     server.URL + "/github/search/repositories",
		ROSPackages:        server.URL + "/ros/rolling/distribution.yaml",
		OpenReviewEmbodied: server.URL + "/openreview/notes/search",
	})
	for _, tc := range []struct {
		database string
		query    string
		title    string
	}{
		{"hf_robotics_datasets", "lerobot", "lerobot/pusht"},
		{"github_robotics", "lerobot", "huggingface/lerobot"},
		{"ros_packages", "nav2_bringup", "nav2_bringup"},
		{"openreview_embodied", "embodied ai", "InteriorSim: A Photorealistic Simulator for Embodied AI"},
	} {
		hits, err := registry.Search(context.Background(), science.SearchRequest{Database: tc.database, Query: tc.query, Limit: 2})
		if err != nil {
			t.Fatalf("search %s: %v", tc.database, err)
		}
		if len(hits) != 1 || hits[0].Title != tc.title || hits[0].Metadata["database"] != tc.database || hits[0].Provenance.URL == "" {
			t.Fatalf("%s hits = %+v", tc.database, hits)
		}
	}
}

func TestSearchUsesLiveChemistryAndPharmacologyEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bindingdb/getLigandsByUniprot":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"monomerid":     "5001",
				"smiles":        "CCOc1ccc2ncnc(Nc3cccc(Cl)c3)c2c1",
				"targetName":    "Epidermal growth factor receptor",
				"affinity_type": "IC50",
				"affinity":      "42 nM",
				"uniprot":       "P00533",
			}})
		case "/chebi/es_search/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{
					"_score": 7.5,
					"_source": map[string]any{
						"chebi_accession": "CHEBI:49668",
						"name":            "gefitinib",
						"definition":      "An anilinoquinazoline EGFR inhibitor.",
						"formula":         "C22H24ClFN4O3",
					},
				}},
			})
		case "/gtopdb/ligands":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"ligandId":  "4924",
				"name":      "gefitinib",
				"type":      "Synthetic organic",
				"approved":  true,
				"inchikey":  "XGALLCVXEZPNRQ-UHFFFAOYSA-N",
				"pubchemId": 123631,
			}})
		case "/surechembl/search/structure":
			if r.Method != http.MethodPost {
				t.Fatalf("surechembl search method = %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"hash": "job-1"})
		case "/surechembl/search/job-1/results":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": map[string]any{
					"documents": []map[string]any{{
						"docId": "WO2026000001",
						"metadata": map[string]any{
							"titles": []map[string]any{{
								"lang":   "en",
								"titles": []string{"EGFR inhibitor patent"},
							}},
							"abstracts": []map[string]any{{
								"lang":      "en",
								"abstracts": []string{"Patent abstract mentioning gefitinib."},
							}},
						},
					}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	registry := science.NewRegistryWithEndpoints(server.Client(), science.Endpoints{
		BindingDB:      server.URL + "/bindingdb",
		ChEBI:          server.URL + "/chebi",
		GtoPDB:         server.URL + "/gtopdb",
		SureChEMBL:     server.URL + "/surechembl",
		ClinicalTrials: server.URL + "/unused",
	})
	for _, tc := range []struct {
		database string
		query    string
		title    string
	}{
		{"bindingdb", "P00533", "Epidermal growth factor receptor"},
		{"chebi", "gefitinib", "gefitinib"},
		{"gtopdb", "gefitinib", "gefitinib"},
		{"surechembl", "gefitinib", "EGFR inhibitor patent"},
	} {
		hits, err := registry.Search(context.Background(), science.SearchRequest{Database: tc.database, Query: tc.query, Limit: 2})
		if err != nil {
			t.Fatalf("search %s: %v", tc.database, err)
		}
		if len(hits) != 1 || hits[0].Title != tc.title || hits[0].Metadata["database"] != tc.database || hits[0].Provenance.URL == "" {
			t.Fatalf("%s hits = %+v", tc.database, hits)
		}
	}
}

func TestSearchUsesLiveVariantAndGenomeBrowserEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dbsnp/esearch.fcgi":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"esearchresult": map[string]any{"idlist": []string{"7412"}},
			})
		case "/dbsnp/esummary.fcgi":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"uids": []string{"7412"},
					"7412": map[string]any{
						"refsnp_id":   "7412",
						"snp_class":   "snp",
						"chr":         "7",
						"chrpos":      "7:55249071",
						"global_mafs": []map[string]any{{"study": "1000Genomes", "freq": "0.091"}},
					},
				},
			})
		case "/gnomad":
			if r.Method != http.MethodPost {
				t.Fatalf("gnomad method = %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"variant": map[string]any{
						"variantId":        "7-55249071-G-A",
						"reference_genome": "GRCh38",
						"chrom":            "7",
						"pos":              55249071,
						"ref":              "G",
						"alt":              "A",
						"genome": map[string]any{
							"ac": 23,
							"an": 200000,
							"af": 0.000115,
						},
						"exome": map[string]any{
							"ac": 12,
							"an": 120000,
							"af": 0.0001,
						},
					},
				},
			})
		case "/myvariant/v1/query":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"hits": []map[string]any{{
					"_id":    "chr7:g.55249071G>A",
					"_score": 42.0,
					"dbsnp":  map[string]any{"rsid": "rs121913438"},
					"clinvar": map[string]any{
						"rcv": map[string]any{
							"conditions": map[string]any{"name": "lung adenocarcinoma"},
						},
					},
					"cadd": map[string]any{"phred": 27.4},
				}},
			})
		case "/ucsc/search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"positionMatches": []map[string]any{{
					"position":    "chr7:55,249,071-55,249,071",
					"description": "EGFR locus",
					"genome":      "hg38",
				}},
				"trackMatches": []map[string]any{{
					"track":       "knownGene",
					"shortLabel":  "Known Genes",
					"longLabel":   "UCSC known gene annotations",
					"description": "Known gene track",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	registry := science.NewRegistryWithEndpoints(server.Client(), science.Endpoints{
		DbSNP:     server.URL + "/dbsnp",
		GnomAD:    server.URL + "/gnomad",
		MyVariant: server.URL + "/myvariant/v1/query",
		UCSC:      server.URL + "/ucsc/search",
	})
	for _, tc := range []struct {
		database string
		query    string
		title    string
	}{
		{"dbsnp", "rs7412", "rs7412"},
		{"gnomad", "7-55249071-G-A", "7-55249071-G-A"},
		{"myvariant", "rs121913438", "rs121913438"},
		{"ucsc", "EGFR", "chr7:55,249,071-55,249,071"},
	} {
		hits, err := registry.Search(context.Background(), science.SearchRequest{Database: tc.database, Query: tc.query, Limit: 2})
		if err != nil {
			t.Fatalf("search %s: %v", tc.database, err)
		}
		if len(hits) != 1 || hits[0].Title != tc.title || hits[0].Metadata["database"] != tc.database || hits[0].Provenance.URL == "" {
			t.Fatalf("%s hits = %+v", tc.database, hits)
		}
	}
}

func TestSearchUsesLiveOmicsEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/geo/esearch.fcgi":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"esearchresult": map[string]any{"idlist": []string{"200000001"}},
			})
		case "/geo/esummary.fcgi":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"uids": []string{"200000001"},
					"200000001": map[string]any{
						"accession": "GSE12345",
						"title":     "EGFR expression in lung cancer",
						"summary":   "RNA-seq expression profiling dataset",
						"gdsType":   "Expression profiling by high throughput sequencing",
						"taxon":     "Homo sapiens",
					},
				},
			})
		case "/gtex/reference/geneSearch":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{
					"gencodeId":   "ENSG00000146648.15",
					"geneSymbol":  "EGFR",
					"description": "epidermal growth factor receptor",
				}},
			})
		case "/gtex/expression/medianGeneExpression":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{
					"gencodeId":          "ENSG00000146648.15",
					"geneSymbol":         "EGFR",
					"tissueSiteDetailId": "Lung",
					"median":             22.3,
					"unit":               "TPM",
				}},
			})
		case "/hpa/search_download.php":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"Gene":                   "ENSG00000146648",
				"Gene name":              "EGFR",
				"Protein class":          "Predicted intracellular proteins",
				"RNA tissue specificity": "Tissue enhanced",
				"Uniprot":                "P00533",
			}})
		case "/depmap/genes":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{
					"id":   "EGFR",
					"type": "gene",
					"attributes": map[string]any{
						"symbol":           "EGFR",
						"hgnc_id":          "HGNC:3236",
						"location":         "7p11.2",
						"cancer_driver":    true,
						"method_of_action": "oncogene",
					},
				}},
			})
		case "/gxa/json/experiments":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"experiments": []map[string]any{{
					"experimentAccession":   "E-GEOD-12345",
					"experimentDescription": "Bulk EGFR expression atlas study",
					"species":               "Homo sapiens",
					"experimentType":        "Baseline",
					"technologyType":        []string{"RNA-Seq mRNA"},
					"numberOfAssays":        48,
				}},
			})
		case "/gxa/sc/json/experiments":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"experiments": []map[string]any{{
					"experimentAccession":   "E-CELL-1",
					"experimentDescription": "Single-cell EGFR lung atlas",
					"species":               "Homo sapiens",
					"experimentType":        "Baseline",
					"technologyType":        []string{"10xv3"},
					"numberOfAssays":        584884,
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	registry := science.NewRegistryWithEndpoints(server.Client(), science.Endpoints{
		GEO:             server.URL + "/geo",
		GTEx:            server.URL + "/gtex",
		HPA:             server.URL + "/hpa/search_download.php",
		DepMap:          server.URL + "/depmap",
		ExpressionAtlas: server.URL + "/gxa/json/experiments",
		SingleCellAtlas: server.URL + "/gxa/sc/json/experiments",
	})
	for _, tc := range []struct {
		database string
		query    string
		title    string
	}{
		{"geo", "EGFR", "EGFR expression in lung cancer"},
		{"gtex", "EGFR", "EGFR in Lung"},
		{"hpa", "EGFR", "EGFR"},
		{"depmap", "EGFR", "EGFR"},
		{"expression_atlas", "EGFR", "Bulk EGFR expression atlas study"},
		{"single_cell_atlas", "EGFR", "Single-cell EGFR lung atlas"},
	} {
		hits, err := registry.Search(context.Background(), science.SearchRequest{Database: tc.database, Query: tc.query, Limit: 2})
		if err != nil {
			t.Fatalf("search %s: %v", tc.database, err)
		}
		if len(hits) != 1 || hits[0].Title != tc.title || hits[0].Metadata["database"] != tc.database || hits[0].Provenance.URL == "" {
			t.Fatalf("%s hits = %+v", tc.database, hits)
		}
	}
}

func TestSearchUsesLiveNetworkPathwayProteinAndStructureEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/biogrid/interactions":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"BIOGRID_INTERACTION_ID": "1001",
				"OFFICIAL_SYMBOL_A":      "EGFR",
				"OFFICIAL_SYMBOL_B":      "GRB2",
				"EXPERIMENTAL_SYSTEM":    "Affinity Capture-Western",
				"THROUGHPUT":             "Low Throughput",
				"PUBMED_ID":              "123456",
			}})
		case "/intact/search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{
					"interactionAcs":              []string{"EBI-1"},
					"moleculeA":                   "EGFR",
					"moleculeB":                   "GRB2",
					"interactionType":             "physical association",
					"publicationPubmedIdentifier": "123456",
				}},
			})
		case "/kegg/find/pathway/EGFR":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("path:hsa04012\tErbB signaling pathway - Homo sapiens (human)\n"))
		case "/opentargets":
			if r.Method != http.MethodPost {
				t.Fatalf("opentargets method = %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"search": map[string]any{
						"hits": []map[string]any{{
							"id":          "ENSG00000146648",
							"entity":      "target",
							"name":        "EGFR",
							"description": "epidermal growth factor receptor",
						}},
					},
				},
			})
		case "/wikipathways/findPathwaysByText":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": []map[string]any{{
					"id":      "WP673",
					"name":    "ErbB signaling pathway",
					"species": "Homo sapiens",
					"url":     "https://www.wikipathways.org/pathways/WP673.html",
					"score":   0.98,
				}},
			})
		case "/interpro/api/entry/interpro/":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{
					"metadata": map[string]any{
						"accession":       "IPR000719",
						"name":            "Protein kinase domain",
						"type":            "domain",
						"source_database": "interpro",
					},
					"proteins_count": 12345,
				}},
			})
		case "/pdbe/search/pdb/select":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"response": map[string]any{
					"docs": []map[string]any{{
						"pdb_id":              "1M17",
						"title":               "EGFR kinase domain",
						"experimental_method": "X-ray diffraction",
						"resolution":          2.6,
					}},
				},
			})
		case "/sifts/api/mappings/uniprot/P00533":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"P00533": map[string]any{
					"PDB": map[string]any{
						"1M17": []map[string]any{{
							"chain_id":  "A",
							"start":     1,
							"end":       300,
							"unp_start": 712,
							"unp_end":   1000,
						}},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	registry := science.NewRegistryWithEndpoints(server.Client(), science.Endpoints{
		BioGRID:      server.URL + "/biogrid/interactions",
		IntAct:       server.URL + "/intact/search",
		KEGG:         server.URL + "/kegg",
		OpenTargets:  server.URL + "/opentargets",
		WikiPathways: server.URL + "/wikipathways",
		InterPro:     server.URL + "/interpro/api/entry/interpro/",
		PDBe:         server.URL + "/pdbe/search/pdb/select",
		SIFTS:        server.URL + "/sifts/api/mappings/uniprot",
	})
	for _, tc := range []struct {
		database string
		query    string
		title    string
	}{
		{"biogrid", "EGFR", "EGFR-GRB2"},
		{"intact", "EGFR", "EGFR-GRB2"},
		{"kegg", "EGFR", "ErbB signaling pathway - Homo sapiens (human)"},
		{"opentargets", "EGFR", "EGFR"},
		{"wikipathways", "EGFR", "ErbB signaling pathway"},
		{"interpro", "EGFR", "Protein kinase domain"},
		{"pdbe", "EGFR", "EGFR kinase domain"},
		{"sifts", "P00533", "P00533 -> 1M17"},
	} {
		hits, err := registry.Search(context.Background(), science.SearchRequest{Database: tc.database, Query: tc.query, Limit: 2})
		if err != nil {
			t.Fatalf("search %s: %v", tc.database, err)
		}
		if len(hits) != 1 || hits[0].Title != tc.title || hits[0].Metadata["database"] != tc.database || hits[0].Provenance.URL == "" {
			t.Fatalf("%s hits = %+v", tc.database, hits)
		}
	}
}

func TestSearchReturnsExpandedConnectorFailuresWithoutFabricatedHits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "offline fixture", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	registry := science.NewRegistryWithEndpoints(server.Client(), science.Endpoints{
		OpenAlex:        server.URL,
		SemanticScholar: server.URL,
		PDB:             server.URL,
		AlphaFold:       server.URL,
		ChEMBL:          server.URL,
		ClinicalTrials:  server.URL,
	})
	for _, database := range []string{"openalex", "semantic_scholar", "pdb", "alphafold", "chembl", "clinicaltrials"} {
		hits, err := registry.Search(context.Background(), science.SearchRequest{
			Database: database,
			Query:    "EGFR inhibitor",
			Limit:    2,
		})
		if err == nil {
			t.Fatalf("search %s fabricated successful hits: %+v", database, hits)
		}
		if len(hits) != 0 {
			t.Fatalf("search %s returned fabricated hits: %+v", database, hits)
		}
	}
}

func TestSearchReturnsConnectorErrorWithoutFallbackHits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "offline fixture", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	registry := science.NewRegistryWithEndpoints(server.Client(), science.Endpoints{PubMed: server.URL})
	hits, err := registry.Search(context.Background(), science.SearchRequest{
		Database: "pubmed",
		Query:    "EGFR inhibitor",
		Limit:    3,
	})
	if err == nil {
		t.Fatalf("search fabricated success: %+v", hits)
	}
	if len(hits) != 0 {
		t.Fatalf("search returned fabricated hits: %+v", hits)
	}
}

func TestSearchUsesLivePubMedEndpointWhenAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/esearch.fcgi":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"esearchresult": map[string]any{"idlist": []string{"123"}},
			})
		case "/esummary.fcgi":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"uids": []string{"123"},
					"123":  map[string]any{"title": "EGFR inhibitor clinical response", "source": "PubMed", "pubdate": "2026"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	registry := science.NewRegistryWithEndpoints(server.Client(), science.Endpoints{PubMed: server.URL})
	hits, err := registry.Search(context.Background(), science.SearchRequest{Database: "pubmed", Query: "EGFR", Limit: 3})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].Title != "EGFR inhibitor clinical response" || hits[0].Provenance.URL == "" {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestSearchUsesLiveUniProtEndpointWhenAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{
					"primaryAccession": "P00533",
					"uniProtkbId":      "EGFR_HUMAN",
					"proteinDescription": map[string]any{
						"recommendedName": map[string]any{
							"fullName": map[string]any{"value": "Epidermal growth factor receptor"},
						},
					},
					"organism": map[string]any{"scientificName": "Homo sapiens"},
				},
			},
		})
	}))
	defer server.Close()

	registry := science.NewRegistryWithEndpoints(server.Client(), science.Endpoints{UniProt: server.URL})
	hits, err := registry.Search(context.Background(), science.SearchRequest{Database: "uniprot", Query: "EGFR", Limit: 3})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "P00533" || hits[0].Title != "EGFR_HUMAN" {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestSearchUsesExpandedLiteratureEndpointsWhenAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/openalex":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{
					"id":                      "https://openalex.org/W1",
					"display_name":            "OpenAlex EGFR paper",
					"publication_year":        2026,
					"cited_by_count":          12,
					"abstract_inverted_index": map[string]any{"EGFR": []int{0}, "paper": []int{1}},
				}},
			})
		case "/crossref":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": map[string]any{"items": []map[string]any{{
					"DOI":       "10.1000/egfr",
					"title":     []string{"Crossref EGFR paper"},
					"publisher": "Journal",
				}}},
			})
		case "/semantic":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{
					"paperId":  "S2-1",
					"title":    "Semantic Scholar EGFR paper",
					"abstract": "EGFR abstract",
					"url":      "https://semanticscholar.org/paper/S2-1",
				}},
			})
		case "/europepmc":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resultList": map[string]any{"result": []map[string]any{{
					"id":           "PMC1",
					"title":        "Europe PMC EGFR paper",
					"abstractText": "EGFR text",
					"journalTitle": "Europe PMC",
				}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	registry := science.NewRegistryWithEndpoints(server.Client(), science.Endpoints{
		OpenAlex:        server.URL + "/openalex",
		Crossref:        server.URL + "/crossref",
		SemanticScholar: server.URL + "/semantic",
		EuropePMC:       server.URL + "/europepmc",
	})
	for _, tc := range []struct {
		database string
		title    string
	}{
		{"openalex", "OpenAlex EGFR paper"},
		{"crossref", "Crossref EGFR paper"},
		{"semantic_scholar", "Semantic Scholar EGFR paper"},
		{"europe_pmc", "Europe PMC EGFR paper"},
	} {
		hits, err := registry.Search(context.Background(), science.SearchRequest{Database: tc.database, Query: "EGFR", Limit: 2})
		if err != nil {
			t.Fatalf("search %s: %v", tc.database, err)
		}
		if len(hits) != 1 || hits[0].Title != tc.title || hits[0].Provenance.URL == "" {
			t.Fatalf("%s hits = %+v", tc.database, hits)
		}
	}
}
