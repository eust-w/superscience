package science

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type Database struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Domain      string `json:"domain"`
	Description string `json:"description"`
}

type Provenance struct {
	Source string `json:"source"`
	URL    string `json:"url"`
}

type Hit struct {
	ID         string            `json:"id"`
	Source     string            `json:"source"`
	Title      string            `json:"title"`
	Summary    string            `json:"summary"`
	URL        string            `json:"url"`
	Score      float64           `json:"score"`
	Metadata   map[string]string `json:"metadata"`
	Provenance Provenance        `json:"provenance"`
}

type SearchRequest struct {
	Database string `json:"database"`
	Query    string `json:"query"`
	Limit    int    `json:"limit"`
	Organism string `json:"organism,omitempty"`
}

type Registry struct {
	client    *http.Client
	endpoints Endpoints
	dbs       []Database
}

type Endpoints struct {
	PubMed             string
	Arxiv              string
	UniProt            string
	PubChem            string
	OpenAlex           string
	SemanticScholar    string
	Crossref           string
	EuropePMC          string
	PDB                string
	AlphaFold          string
	ChEMBL             string
	Ensembl            string
	BindingDB          string
	ChEBI              string
	GtoPDB             string
	SureChEMBL         string
	DbSNP              string
	GnomAD             string
	MyVariant          string
	UCSC               string
	GEO                string
	GTEx               string
	HPA                string
	DepMap             string
	ExpressionAtlas    string
	SingleCellAtlas    string
	BioGRID            string
	IntAct             string
	KEGG               string
	OpenTargets        string
	ClinVar            string
	MyGene             string
	Reactome           string
	STRING             string
	WikiPathways       string
	InterPro           string
	PDBe               string
	SIFTS              string
	BioRxiv            string
	ClinicalTrials     string
	NCBIGene           string
	NCBIProtein        string
	DataCite           string
	Zenodo             string
	NASAExoplanet      string
	INSPIREHEP         string
	NOMADMaterials     string
	HFRoboticsDatasets string
	GitHubRobotics     string
	ROSPackages        string
	OpenReviewEmbodied string
}

func NewRegistry(client *http.Client) *Registry {
	return NewRegistryWithEndpoints(client, Endpoints{
		PubMed:             "https://eutils.ncbi.nlm.nih.gov/entrez/eutils",
		Arxiv:              "https://export.arxiv.org/api/query",
		UniProt:            "https://rest.uniprot.org/uniprotkb/search",
		PubChem:            "https://pubchem.ncbi.nlm.nih.gov/rest/pug",
		OpenAlex:           "https://api.openalex.org/works",
		SemanticScholar:    "https://api.semanticscholar.org/graph/v1/paper/search",
		Crossref:           "https://api.crossref.org/works",
		EuropePMC:          "https://www.ebi.ac.uk/europepmc/webservices/rest/search",
		PDB:                "https://search.rcsb.org/rcsbsearch/v2/query",
		AlphaFold:          "https://alphafold.ebi.ac.uk/api/search",
		ChEMBL:             "https://www.ebi.ac.uk/chembl/api/data",
		Ensembl:            "https://rest.ensembl.org",
		BindingDB:          "https://www.bindingdb.org/rest",
		ChEBI:              "https://www.ebi.ac.uk/chebi/backend/api/public",
		GtoPDB:             "https://www.guidetopharmacology.org/services",
		SureChEMBL:         "https://surechembl.org/api",
		DbSNP:              "https://eutils.ncbi.nlm.nih.gov/entrez/eutils",
		GnomAD:             "https://gnomad.broadinstitute.org/api/",
		MyVariant:          "https://myvariant.info/v1/query",
		UCSC:               "https://api.genome.ucsc.edu/search",
		GEO:                "https://eutils.ncbi.nlm.nih.gov/entrez/eutils",
		GTEx:               "https://gtexportal.org/api/v2",
		HPA:                "https://www.proteinatlas.org/api/search_download.php",
		DepMap:             "https://api.cellmodelpassports.sanger.ac.uk",
		ExpressionAtlas:    "https://www.ebi.ac.uk/gxa/json/experiments",
		SingleCellAtlas:    "https://www.ebi.ac.uk/gxa/sc/json/experiments",
		BioGRID:            "https://webservice.thebiogrid.org/interactions",
		IntAct:             "https://www.ebi.ac.uk/intact/ws/interactor/findInteractor",
		KEGG:               "https://rest.kegg.jp",
		OpenTargets:        "https://api.platform.opentargets.org/api/v4/graphql",
		ClinVar:            "https://eutils.ncbi.nlm.nih.gov/entrez/eutils",
		MyGene:             "https://mygene.info/v3/query",
		Reactome:           "https://reactome.org/ContentService",
		STRING:             "https://string-db.org/api/json/interaction_partners",
		WikiPathways:       "https://webservice.wikipathways.org",
		InterPro:           "https://www.ebi.ac.uk/interpro/api/entry/interpro/",
		PDBe:               "https://www.ebi.ac.uk/pdbe/search/pdb/select",
		SIFTS:              "https://www.ebi.ac.uk/pdbe/api/mappings/best_structures",
		BioRxiv:            "https://api.biorxiv.org/details/biorxiv",
		ClinicalTrials:     "https://clinicaltrials.gov/api/v2/studies",
		NCBIGene:           "https://eutils.ncbi.nlm.nih.gov/entrez/eutils",
		NCBIProtein:        "https://eutils.ncbi.nlm.nih.gov/entrez/eutils",
		DataCite:           "https://api.datacite.org/dois",
		Zenodo:             "https://zenodo.org/api/records",
		NASAExoplanet:      "https://exoplanetarchive.ipac.caltech.edu/TAP/sync",
		INSPIREHEP:         "https://inspirehep.net/api/literature",
		NOMADMaterials:     "https://nomad-lab.eu/prod/v1/api/v1",
		HFRoboticsDatasets: "https://huggingface.co/api/datasets",
		GitHubRobotics:     "https://api.github.com/search/repositories",
		ROSPackages:        "https://raw.githubusercontent.com/ros/rosdistro/master/rolling/distribution.yaml",
		OpenReviewEmbodied: "https://api.openreview.net/notes/search",
	})
}

func NewRegistryWithEndpoints(client *http.Client, endpoints Endpoints) *Registry {
	if client == nil {
		client = http.DefaultClient
	}
	return &Registry{
		client:    client,
		endpoints: endpoints,
		dbs: []Database{
			{ID: "pubmed", Name: "PubMed", Domain: "literature", Description: "Biomedical literature citations and abstracts"},
			{ID: "arxiv", Name: "arXiv", Domain: "literature", Description: "Open preprints for physics, math, computer science, and quantitative biology"},
			{ID: "uniprot", Name: "UniProt", Domain: "proteins", Description: "Protein sequence and functional annotation"},
			{ID: "pubchem", Name: "PubChem", Domain: "chemistry", Description: "Chemical substances, compounds, and bioassays"},
			{ID: "openalex", Name: "OpenAlex", Domain: "literature", Description: "Open scholarly works, authors, institutions, and concepts"},
			{ID: "semantic_scholar", Name: "Semantic Scholar", Domain: "literature", Description: "Paper search, citations, abstracts, and author metadata"},
			{ID: "crossref", Name: "Crossref", Domain: "literature", Description: "DOI metadata for scholarly publications"},
			{ID: "europe_pmc", Name: "Europe PMC", Domain: "literature", Description: "Life-science literature and full-text metadata"},
			{ID: "pdb", Name: "Protein Data Bank", Domain: "structures", Description: "Experimentally resolved biomolecular structures"},
			{ID: "alphafold", Name: "AlphaFold DB", Domain: "structures", Description: "Predicted protein structures and confidence metadata"},
			{ID: "chembl", Name: "ChEMBL", Domain: "chemistry", Description: "Bioactive molecules, assays, targets, and drug-like compounds"},
			{ID: "ensembl", Name: "Ensembl", Domain: "genomics", Description: "Genome annotation, genes, variants, and comparative genomics"},
			{ID: "clinicaltrials", Name: "ClinicalTrials.gov", Domain: "clinical", Description: "Clinical study records and intervention metadata"},
			{ID: "ncbi_gene", Name: "NCBI Gene", Domain: "genomics", Description: "Gene-centered records, aliases, and organism metadata"},
			{ID: "ncbi_protein", Name: "NCBI Protein", Domain: "proteins", Description: "Protein records from NCBI Entrez"},
			{ID: "bindingdb", Name: "BindingDB", Domain: "chemistry", Description: "Measured protein-ligand binding affinities"},
			{ID: "chebi", Name: "ChEBI", Domain: "chemistry", Description: "Chemical entities of biological interest"},
			{ID: "gtopdb", Name: "Guide to Pharmacology", Domain: "pharmacology", Description: "Drug targets, ligands, and pharmacology"},
			{ID: "surechembl", Name: "SureChEMBL", Domain: "chemistry", Description: "Patent chemistry search and compound metadata"},
			{ID: "clinvar", Name: "ClinVar", Domain: "genomics", Description: "Clinical variant interpretations"},
			{ID: "dbsnp", Name: "dbSNP", Domain: "genomics", Description: "Short genetic variation records"},
			{ID: "gnomad", Name: "gnomAD", Domain: "genomics", Description: "Population variant frequencies"},
			{ID: "mygene", Name: "MyGene.info", Domain: "genomics", Description: "Gene annotation aggregation"},
			{ID: "myvariant", Name: "MyVariant.info", Domain: "genomics", Description: "Variant annotation aggregation"},
			{ID: "ucsc", Name: "UCSC Genome Browser", Domain: "genomics", Description: "Genome tracks and annotation tables"},
			{ID: "geo", Name: "NCBI GEO", Domain: "omics", Description: "Gene expression datasets and series"},
			{ID: "gtex", Name: "GTEx", Domain: "omics", Description: "Tissue expression and eQTL resources"},
			{ID: "hpa", Name: "Human Protein Atlas", Domain: "omics", Description: "Protein expression and localization"},
			{ID: "depmap", Name: "DepMap", Domain: "omics", Description: "Cancer dependency and perturbation datasets"},
			{ID: "expression_atlas", Name: "Expression Atlas", Domain: "omics", Description: "Differential and baseline expression datasets"},
			{ID: "single_cell_atlas", Name: "Single Cell Expression Atlas", Domain: "omics", Description: "Single-cell expression experiments"},
			{ID: "biogrid", Name: "BioGRID", Domain: "networks", Description: "Protein and genetic interactions"},
			{ID: "intact", Name: "IntAct", Domain: "networks", Description: "Molecular interaction records"},
			{ID: "kegg", Name: "KEGG", Domain: "pathways", Description: "Genes, pathways, diseases, and compounds"},
			{ID: "opentargets", Name: "Open Targets", Domain: "targets", Description: "Target-disease evidence and tractability"},
			{ID: "reactome", Name: "Reactome", Domain: "pathways", Description: "Curated biological pathways"},
			{ID: "string", Name: "STRING", Domain: "networks", Description: "Functional protein association networks"},
			{ID: "wikipathways", Name: "WikiPathways", Domain: "pathways", Description: "Community-curated pathway models"},
			{ID: "interpro", Name: "InterPro", Domain: "proteins", Description: "Protein families, domains, and sites"},
			{ID: "pdbe", Name: "PDBe", Domain: "structures", Description: "Protein Data Bank in Europe structure metadata"},
			{ID: "sifts", Name: "SIFTS", Domain: "structures", Description: "Structure-to-sequence residue mappings"},
			{ID: "biorxiv", Name: "bioRxiv", Domain: "literature", Description: "Biology preprints"},
			{ID: "datacite", Name: "DataCite", Domain: "datasets", Description: "Cross-disciplinary DOI metadata for research datasets, software, and outputs"},
			{ID: "zenodo", Name: "Zenodo", Domain: "datasets", Description: "Open research records, datasets, software, and notebooks"},
			{ID: "nasa_exoplanet", Name: "NASA Exoplanet Archive", Domain: "astronomy", Description: "Confirmed exoplanets and planetary system metadata"},
			{ID: "inspire_hep", Name: "INSPIRE HEP", Domain: "physics", Description: "High-energy physics literature and citation metadata"},
			{ID: "nomad_materials", Name: "NOMAD Materials", Domain: "materials", Description: "FAIR computational and experimental materials science entries"},
			{ID: "hf_robotics_datasets", Name: "Hugging Face Robotics Datasets", Domain: "embodied_ai", Description: "Robot learning and embodied AI datasets on Hugging Face Hub"},
			{ID: "github_robotics", Name: "GitHub Robotics", Domain: "robotics", Description: "Open-source robotics, robot learning, and embodied AI repositories"},
			{ID: "ros_packages", Name: "ROS Packages", Domain: "robotics", Description: "ROS and ROS 2 package metadata from ROS Index and rosdistro"},
			{ID: "openreview_embodied", Name: "OpenReview Embodied AI", Domain: "embodied_ai", Description: "Embodied AI, robot learning, and simulator submissions from OpenReview"},
		},
	}
}

func (r *Registry) Databases() []Database {
	result := make([]Database, len(r.dbs))
	copy(result, r.dbs)
	return result
}

func (r *Registry) Search(ctx context.Context, request SearchRequest) ([]Hit, error) {
	_ = r.client
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit := request.Limit
	if limit <= 0 || limit > 25 {
		limit = 10
	}
	db, ok := r.database(request.Database)
	if !ok {
		return nil, fmt.Errorf("unknown science database %q", request.Database)
	}
	query := strings.TrimSpace(request.Query)
	if query == "" {
		return nil, errors.New("science search query is required")
	}
	hits, err := r.liveSearch(ctx, db, query, limit, request.Organism)
	if err != nil {
		return nil, fmt.Errorf("%s connector search failed: %w", db.ID, err)
	}
	return hits, nil
}

func (r *Registry) liveSearch(ctx context.Context, db Database, query string, limit int, organism string) ([]Hit, error) {
	switch db.ID {
	case "pubmed":
		return r.searchPubMed(ctx, query, limit)
	case "arxiv":
		return r.searchArxiv(ctx, query, limit)
	case "uniprot":
		return r.searchUniProt(ctx, query, limit, organism)
	case "pubchem":
		return r.searchPubChem(ctx, query, limit)
	case "openalex":
		return r.searchOpenAlex(ctx, query, limit)
	case "crossref":
		return r.searchCrossref(ctx, query, limit)
	case "semantic_scholar":
		return r.searchSemanticScholar(ctx, query, limit)
	case "europe_pmc":
		return r.searchEuropePMC(ctx, query, limit)
	case "pdb":
		return r.searchPDB(ctx, query, limit)
	case "alphafold":
		return r.searchAlphaFold(ctx, query, limit)
	case "chembl":
		return r.searchChEMBL(ctx, query, limit)
	case "bindingdb":
		return r.searchBindingDB(ctx, query, limit)
	case "chebi":
		return r.searchChEBI(ctx, query, limit)
	case "gtopdb":
		return r.searchGtoPDB(ctx, query, limit)
	case "surechembl":
		return r.searchSureChEMBL(ctx, query, limit)
	case "ensembl":
		return r.searchEnsembl(ctx, query, limit, organism)
	case "clinvar":
		return r.searchClinVar(ctx, query, limit)
	case "dbsnp":
		return r.searchDbSNP(ctx, query, limit)
	case "gnomad":
		return r.searchGnomAD(ctx, query, limit)
	case "mygene":
		return r.searchMyGene(ctx, query, limit, organism)
	case "myvariant":
		return r.searchMyVariant(ctx, query, limit)
	case "ucsc":
		return r.searchUCSC(ctx, query, limit, organism)
	case "geo":
		return r.searchGEO(ctx, query, limit)
	case "gtex":
		return r.searchGTEx(ctx, query, limit)
	case "hpa":
		return r.searchHPA(ctx, query, limit)
	case "depmap":
		return r.searchDepMap(ctx, query, limit)
	case "expression_atlas":
		return r.searchExpressionAtlas(ctx, query, limit)
	case "single_cell_atlas":
		return r.searchSingleCellAtlas(ctx, query, limit)
	case "biogrid":
		return r.searchBioGRID(ctx, query, limit)
	case "intact":
		return r.searchIntAct(ctx, query, limit)
	case "kegg":
		return r.searchKEGG(ctx, query, limit)
	case "opentargets":
		return r.searchOpenTargets(ctx, query, limit)
	case "reactome":
		return r.searchReactome(ctx, query, limit, organism)
	case "string":
		return r.searchSTRING(ctx, query, limit, organism)
	case "wikipathways":
		return r.searchWikiPathways(ctx, query, limit)
	case "interpro":
		return r.searchInterPro(ctx, query, limit)
	case "pdbe":
		return r.searchPDBe(ctx, query, limit)
	case "sifts":
		return r.searchSIFTS(ctx, query, limit)
	case "biorxiv":
		return r.searchBioRxiv(ctx, query, limit)
	case "clinicaltrials":
		return r.searchClinicalTrials(ctx, query, limit)
	case "ncbi_gene":
		return r.searchNCBIGene(ctx, query, limit, organism)
	case "ncbi_protein":
		return r.searchNCBIProtein(ctx, query, limit)
	case "datacite":
		return r.searchDataCite(ctx, query, limit)
	case "zenodo":
		return r.searchZenodo(ctx, query, limit)
	case "nasa_exoplanet":
		return r.searchNASAExoplanet(ctx, query, limit)
	case "inspire_hep":
		return r.searchINSPIREHEP(ctx, query, limit)
	case "nomad_materials":
		return r.searchNOMADMaterials(ctx, query, limit)
	case "hf_robotics_datasets":
		return r.searchHFRoboticsDatasets(ctx, query, limit)
	case "github_robotics":
		return r.searchGitHubRobotics(ctx, query, limit)
	case "ros_packages":
		return r.searchROSPackages(ctx, query, limit)
	case "openreview_embodied":
		return r.searchOpenReviewEmbodied(ctx, query, limit)
	default:
		return nil, fmt.Errorf("unsupported database %q", db.ID)
	}
}

func (r *Registry) searchPubMed(ctx context.Context, query string, limit int) ([]Hit, error) {
	base := strings.TrimRight(r.endpoints.PubMed, "/")
	searchURL := base + "/esearch.fcgi?db=pubmed&retmode=json&retmax=" + url.QueryEscape(fmt.Sprint(limit)) + "&term=" + url.QueryEscape(query)
	var search struct {
		Result struct {
			IDList []string `json:"idlist"`
		} `json:"esearchresult"`
	}
	if err := r.getJSON(ctx, searchURL, &search); err != nil {
		return nil, err
	}
	if len(search.Result.IDList) == 0 {
		return nil, nil
	}
	summaryURL := base + "/esummary.fcgi?db=pubmed&retmode=json&id=" + url.QueryEscape(strings.Join(search.Result.IDList, ","))
	var summary struct {
		Result map[string]any `json:"result"`
	}
	if err := r.getJSON(ctx, summaryURL, &summary); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(search.Result.IDList))
	for idx, id := range search.Result.IDList {
		item, _ := summary.Result[id].(map[string]any)
		title, _ := item["title"].(string)
		source, _ := item["source"].(string)
		pubdate, _ := item["pubdate"].(string)
		if title == "" {
			title = "PubMed " + id
		}
		hits = append(hits, Hit{
			ID:      id,
			Source:  "PubMed",
			Title:   title,
			Summary: strings.TrimSpace(source + " " + pubdate),
			URL:     "https://pubmed.ncbi.nlm.nih.gov/" + id + "/",
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database": "pubmed",
				"year":     pubdate,
			},
			Provenance: Provenance{Source: "PubMed", URL: "https://pubmed.ncbi.nlm.nih.gov/" + id + "/"},
		})
	}
	return hits, nil
}

func (r *Registry) searchUniProt(ctx context.Context, query string, limit int, organism string) ([]Hit, error) {
	q := query
	if organism != "" {
		q += " AND organism_name:" + organism
	}
	searchURL := strings.TrimRight(r.endpoints.UniProt, "/") + "?format=json&size=" + url.QueryEscape(fmt.Sprint(limit)) + "&query=" + url.QueryEscape(q)
	var payload struct {
		Results []struct {
			PrimaryAccession string `json:"primaryAccession"`
			UniProtkbID      string `json:"uniProtkbId"`
			Protein          struct {
				RecommendedName struct {
					FullName struct {
						Value string `json:"value"`
					} `json:"fullName"`
				} `json:"recommendedName"`
			} `json:"proteinDescription"`
			Organism struct {
				ScientificName string `json:"scientificName"`
			} `json:"organism"`
		} `json:"results"`
	}
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(payload.Results))
	for idx, item := range payload.Results {
		title := item.UniProtkbID
		if title == "" {
			title = item.PrimaryAccession
		}
		summary := item.Protein.RecommendedName.FullName.Value
		if item.Organism.ScientificName != "" {
			summary = strings.TrimSpace(summary + " — " + item.Organism.ScientificName)
		}
		url := "https://www.uniprot.org/uniprotkb/" + item.PrimaryAccession + "/entry"
		hits = append(hits, Hit{
			ID:      item.PrimaryAccession,
			Source:  "UniProt",
			Title:   title,
			Summary: summary,
			URL:     url,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database": "uniprot",
				"organism": item.Organism.ScientificName,
			},
			Provenance: Provenance{Source: "UniProt", URL: url},
		})
	}
	return hits, nil
}

func (r *Registry) searchArxiv(ctx context.Context, query string, limit int) ([]Hit, error) {
	searchURL := strings.TrimRight(r.endpoints.Arxiv, "/") + "?search_query=all:" + url.QueryEscape(query) + "&start=0&max_results=" + url.QueryEscape(fmt.Sprint(limit))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, errors.New(resp.Status)
	}
	var feed struct {
		Entries []struct {
			ID      string `xml:"id"`
			Title   string `xml:"title"`
			Summary string `xml:"summary"`
		} `xml:"entry"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(feed.Entries))
	for idx, entry := range feed.Entries {
		hits = append(hits, Hit{
			ID:         entry.ID,
			Source:     "arXiv",
			Title:      strings.Join(strings.Fields(entry.Title), " "),
			Summary:    strings.Join(strings.Fields(entry.Summary), " "),
			URL:        entry.ID,
			Score:      1 / float64(idx+1),
			Metadata:   map[string]string{"database": "arxiv"},
			Provenance: Provenance{Source: "arXiv", URL: entry.ID},
		})
	}
	return hits, nil
}

func (r *Registry) searchPubChem(ctx context.Context, query string, limit int) ([]Hit, error) {
	base := strings.TrimRight(r.endpoints.PubChem, "/")
	searchURL := base + "/compound/name/" + url.PathEscape(query) + "/cids/JSON"
	var cids struct {
		IdentifierList struct {
			CID []int `json:"CID"`
		} `json:"IdentifierList"`
	}
	if err := r.getJSON(ctx, searchURL, &cids); err != nil {
		return nil, err
	}
	if len(cids.IdentifierList.CID) > limit {
		cids.IdentifierList.CID = cids.IdentifierList.CID[:limit]
	}
	hits := make([]Hit, 0, len(cids.IdentifierList.CID))
	for idx, cid := range cids.IdentifierList.CID {
		id := fmt.Sprint(cid)
		url := "https://pubchem.ncbi.nlm.nih.gov/compound/" + id
		hits = append(hits, Hit{
			ID:         id,
			Source:     "PubChem",
			Title:      "PubChem compound " + id,
			Summary:    "Compound identifier returned by PubChem for " + query,
			URL:        url,
			Score:      1 / float64(idx+1),
			Metadata:   map[string]string{"database": "pubchem", "cid": id},
			Provenance: Provenance{Source: "PubChem", URL: url},
		})
	}
	return hits, nil
}

func (r *Registry) searchOpenAlex(ctx context.Context, query string, limit int) ([]Hit, error) {
	searchURL := withQuery(r.endpoints.OpenAlex, map[string]string{
		"search":   query,
		"per-page": fmt.Sprint(limit),
	})
	var payload struct {
		Results []struct {
			ID                    string           `json:"id"`
			DisplayName           string           `json:"display_name"`
			PublicationYear       int              `json:"publication_year"`
			CitedByCount          int              `json:"cited_by_count"`
			AbstractInvertedIndex map[string][]int `json:"abstract_inverted_index"`
		} `json:"results"`
	}
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(payload.Results))
	for idx, item := range payload.Results {
		title := strings.TrimSpace(item.DisplayName)
		if title == "" {
			title = "OpenAlex work " + item.ID
		}
		summary := abstractFromInvertedIndex(item.AbstractInvertedIndex)
		if summary == "" {
			parts := []string{}
			if item.PublicationYear > 0 {
				parts = append(parts, fmt.Sprintf("Published %d", item.PublicationYear))
			}
			if item.CitedByCount > 0 {
				parts = append(parts, fmt.Sprintf("cited by %d works", item.CitedByCount))
			}
			summary = strings.Join(parts, "; ")
		}
		resultURL := item.ID
		if resultURL == "" {
			resultURL = "https://openalex.org"
		}
		hits = append(hits, Hit{
			ID:      item.ID,
			Source:  "OpenAlex",
			Title:   title,
			Summary: summary,
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database":  "openalex",
				"year":      fmt.Sprint(item.PublicationYear),
				"citations": fmt.Sprint(item.CitedByCount),
			},
			Provenance: Provenance{Source: "OpenAlex", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchCrossref(ctx context.Context, query string, limit int) ([]Hit, error) {
	searchURL := withQuery(r.endpoints.Crossref, map[string]string{
		"query": query,
		"rows":  fmt.Sprint(limit),
	})
	var payload struct {
		Message struct {
			Items []struct {
				DOI       string   `json:"DOI"`
				Title     []string `json:"title"`
				Publisher string   `json:"publisher"`
			} `json:"items"`
		} `json:"message"`
	}
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(payload.Message.Items))
	for idx, item := range payload.Message.Items {
		title := firstNonEmpty(item.Title)
		if title == "" {
			title = "Crossref work " + item.DOI
		}
		resultURL := "https://doi.org/" + item.DOI
		if item.DOI == "" {
			resultURL = "https://www.crossref.org"
		}
		hits = append(hits, Hit{
			ID:      item.DOI,
			Source:  "Crossref",
			Title:   title,
			Summary: item.Publisher,
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database":  "crossref",
				"doi":       item.DOI,
				"publisher": item.Publisher,
			},
			Provenance: Provenance{Source: "Crossref", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchSemanticScholar(ctx context.Context, query string, limit int) ([]Hit, error) {
	searchURL := withQuery(r.endpoints.SemanticScholar, map[string]string{
		"query":  query,
		"limit":  fmt.Sprint(limit),
		"fields": "title,abstract,url,paperId,year,venue,citationCount",
	})
	var payload struct {
		Data []struct {
			PaperID       string `json:"paperId"`
			Title         string `json:"title"`
			Abstract      string `json:"abstract"`
			URL           string `json:"url"`
			Year          int    `json:"year"`
			Venue         string `json:"venue"`
			CitationCount int    `json:"citationCount"`
		} `json:"data"`
	}
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(payload.Data))
	for idx, item := range payload.Data {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = "Semantic Scholar paper " + item.PaperID
		}
		resultURL := item.URL
		if resultURL == "" && item.PaperID != "" {
			resultURL = "https://www.semanticscholar.org/paper/" + item.PaperID
		}
		hits = append(hits, Hit{
			ID:      item.PaperID,
			Source:  "Semantic Scholar",
			Title:   title,
			Summary: strings.TrimSpace(item.Abstract),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database":  "semantic_scholar",
				"year":      fmt.Sprint(item.Year),
				"venue":     item.Venue,
				"citations": fmt.Sprint(item.CitationCount),
			},
			Provenance: Provenance{Source: "Semantic Scholar", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchEuropePMC(ctx context.Context, query string, limit int) ([]Hit, error) {
	searchURL := withQuery(r.endpoints.EuropePMC, map[string]string{
		"query":    query,
		"pageSize": fmt.Sprint(limit),
		"format":   "json",
	})
	var payload struct {
		ResultList struct {
			Result []struct {
				ID           string `json:"id"`
				Title        string `json:"title"`
				AbstractText string `json:"abstractText"`
				JournalTitle string `json:"journalTitle"`
				PubYear      string `json:"pubYear"`
			} `json:"result"`
		} `json:"resultList"`
	}
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(payload.ResultList.Result))
	for idx, item := range payload.ResultList.Result {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = "Europe PMC record " + item.ID
		}
		resultURL := europePMCURL(item.ID)
		hits = append(hits, Hit{
			ID:      item.ID,
			Source:  "Europe PMC",
			Title:   title,
			Summary: strings.TrimSpace(item.AbstractText),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database": "europe_pmc",
				"journal":  item.JournalTitle,
				"year":     item.PubYear,
			},
			Provenance: Provenance{Source: "Europe PMC", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchPDB(ctx context.Context, query string, limit int) ([]Hit, error) {
	payload := map[string]any{
		"query": map[string]any{
			"type":       "terminal",
			"service":    "text",
			"parameters": map[string]any{"value": query},
		},
		"request_options": map[string]any{"paginate": map[string]any{"start": 0, "rows": limit}},
		"return_type":     "entry",
	}
	var response struct {
		ResultSet []struct {
			Identifier string  `json:"identifier"`
			Score      float64 `json:"score"`
		} `json:"result_set"`
	}
	if err := r.postJSON(ctx, r.endpoints.PDB, payload, &response); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(response.ResultSet))
	for idx, item := range response.ResultSet {
		resultURL := "https://www.rcsb.org/structure/" + item.Identifier
		hits = append(hits, Hit{
			ID:         item.Identifier,
			Source:     "Protein Data Bank",
			Title:      "PDB structure " + item.Identifier,
			Summary:    "RCSB structure search result for " + query,
			URL:        resultURL,
			Score:      nonZeroScore(item.Score, idx),
			Metadata:   map[string]string{"database": "pdb"},
			Provenance: Provenance{Source: "Protein Data Bank", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchAlphaFold(ctx context.Context, query string, limit int) ([]Hit, error) {
	searchURL := withQuery(r.endpoints.AlphaFold, map[string]string{"query": query, "limit": fmt.Sprint(limit)})
	var payload []struct {
		EntryID                string `json:"entryId"`
		ProteinDescription     string `json:"proteinDescription"`
		Gene                   string `json:"gene"`
		OrganismScientificName string `json:"organismScientificName"`
		UniProtAccession       string `json:"uniprotAccession"`
	}
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	if len(payload) > limit {
		payload = payload[:limit]
	}
	hits := make([]Hit, 0, len(payload))
	for idx, item := range payload {
		title := item.ProteinDescription
		if title == "" {
			title = item.EntryID
		}
		resultURL := "https://alphafold.ebi.ac.uk/entry/" + item.UniProtAccession
		if item.UniProtAccession == "" {
			resultURL = "https://alphafold.ebi.ac.uk"
		}
		hits = append(hits, Hit{
			ID:      item.EntryID,
			Source:  "AlphaFold DB",
			Title:   title,
			Summary: strings.TrimSpace(item.Gene + " " + item.OrganismScientificName),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database": "alphafold",
				"gene":     item.Gene,
				"organism": item.OrganismScientificName,
				"uniprot":  item.UniProtAccession,
			},
			Provenance: Provenance{Source: "AlphaFold DB", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchChEMBL(ctx context.Context, query string, limit int) ([]Hit, error) {
	endpoint := strings.TrimRight(r.endpoints.ChEMBL, "/")
	if strings.HasSuffix(endpoint, "/data") {
		endpoint += "/molecule/search.json"
	}
	searchURL := withQuery(endpoint, map[string]string{"q": query, "limit": fmt.Sprint(limit), "format": "json"})
	var payload struct {
		Molecules []struct {
			ID           string `json:"molecule_chembl_id"`
			PrefName     string `json:"pref_name"`
			MoleculeType string `json:"molecule_type"`
		} `json:"molecules"`
	}
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(payload.Molecules))
	for idx, item := range payload.Molecules {
		title := item.PrefName
		if title == "" {
			title = item.ID
		}
		resultURL := "https://www.ebi.ac.uk/chembl/compound_report_card/" + item.ID + "/"
		hits = append(hits, Hit{
			ID:         item.ID,
			Source:     "ChEMBL",
			Title:      title,
			Summary:    item.MoleculeType,
			URL:        resultURL,
			Score:      1 / float64(idx+1),
			Metadata:   map[string]string{"database": "chembl", "type": item.MoleculeType},
			Provenance: Provenance{Source: "ChEMBL", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchBindingDB(ctx context.Context, query string, limit int) ([]Hit, error) {
	endpoint := strings.TrimRight(r.endpoints.BindingDB, "/")
	if !strings.HasSuffix(endpoint, "/getLigandsByUniprot") {
		endpoint += "/getLigandsByUniprot"
	}
	searchURL := withQuery(endpoint, map[string]string{
		"uniprot":  primarySearchTerm(query) + ";10000",
		"response": "application/json",
	})
	var payload any
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	records := recordsFromAny(payload, "results", "response", "data", "bindings", "ligands")
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, item := range records {
		id := firstField(item, "monomerid", "monomerID", "monomer_id", "bindingdbMonomerID", "id")
		target := firstField(item, "targetName", "target_name", "Target Name", "target", "protein")
		if target == "" {
			target = "BindingDB ligand " + id
		}
		affinity := strings.TrimSpace(firstField(item, "affinity", "affinity_value", "IC50", "Ki", "Kd", "EC50"))
		affinityType := firstField(item, "affinity_type", "affinityType", "type")
		summary := strings.TrimSpace(strings.Join([]string{affinityType, affinity}, " "))
		resultURL := "https://www.bindingdb.org/rwd/bind/chemsearch/marvin/MolStructure.jsp?monomerid=" + url.QueryEscape(id)
		if id == "" {
			resultURL = "https://www.bindingdb.org"
		}
		hits = append(hits, Hit{
			ID:      id,
			Source:  "BindingDB",
			Title:   target,
			Summary: summary,
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database":      "bindingdb",
				"smiles":        firstField(item, "smiles", "SMILES"),
				"affinity":      affinity,
				"affinity_type": affinityType,
				"uniprot":       firstField(item, "uniprot", "UniProt", "uniprot_id"),
			},
			Provenance: Provenance{Source: "BindingDB", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchChEBI(ctx context.Context, query string, limit int) ([]Hit, error) {
	endpoint := strings.TrimRight(r.endpoints.ChEBI, "/")
	if !strings.HasSuffix(endpoint, "/es_search/") {
		endpoint += "/es_search/"
	}
	searchURL := withQuery(endpoint, map[string]string{
		"term": query,
		"size": fmt.Sprint(limit),
	})
	var payload any
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	records := recordsFromAny(payload, "results", "hits", "data")
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, record := range records {
		source := mapFromAny(record["_source"])
		if len(source) == 0 {
			source = record
		}
		id := firstField(source, "chebi_accession", "chebi_id", "id", "accession")
		title := firstField(source, "name", "chebi_name", "label")
		if title == "" {
			title = id
		}
		resultURL := "https://www.ebi.ac.uk/chebi/searchId.do?chebiId=" + url.QueryEscape(id)
		if id == "" {
			resultURL = "https://www.ebi.ac.uk/chebi"
		}
		hits = append(hits, Hit{
			ID:      id,
			Source:  "ChEBI",
			Title:   title,
			Summary: firstField(source, "definition", "description"),
			URL:     resultURL,
			Score:   nonZeroScore(floatFromAny(record["_score"]), idx),
			Metadata: map[string]string{
				"database": "chebi",
				"formula":  firstField(source, "formula", "chemical_formula"),
			},
			Provenance: Provenance{Source: "ChEBI", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchGtoPDB(ctx context.Context, query string, limit int) ([]Hit, error) {
	endpoint := strings.TrimRight(r.endpoints.GtoPDB, "/")
	if !strings.HasSuffix(endpoint, "/ligands") {
		endpoint += "/ligands"
	}
	searchURL := withQuery(endpoint, map[string]string{
		"name": query,
	})
	var payload any
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	records := recordsFromAny(payload, "results", "data", "ligands")
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, item := range records {
		id := firstField(item, "ligandId", "ligand_id", "id")
		title := firstField(item, "name", "title")
		if title == "" {
			title = "GtoPdb ligand " + id
		}
		resultURL := "https://www.guidetopharmacology.org/GRAC/LigandDisplayForward?ligandId=" + url.QueryEscape(id)
		if id == "" {
			resultURL = "https://www.guidetopharmacology.org"
		}
		hits = append(hits, Hit{
			ID:      id,
			Source:  "Guide to Pharmacology",
			Title:   title,
			Summary: firstField(item, "type", "description", "comments"),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database": "gtopdb",
				"type":     firstField(item, "type"),
				"approved": firstField(item, "approved"),
				"inchikey": firstField(item, "inchikey", "inchiKey"),
				"pubchem":  firstField(item, "pubchemId", "pubchem_id"),
			},
			Provenance: Provenance{Source: "Guide to Pharmacology", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchSureChEMBL(ctx context.Context, query string, limit int) ([]Hit, error) {
	base := strings.TrimRight(r.endpoints.SureChEMBL, "/")
	var start struct {
		Hash string `json:"hash"`
		Data struct {
			Hash string `json:"hash"`
		} `json:"data"`
	}
	request := map[string]any{
		"StructureSearchRequest": map[string]any{
			"struct":           "",
			"structSearchType": "text",
			"docSectionFilter": []string{},
			"chemTermFilter":   []string{},
			"options":          map[string]any{},
			"query":            query,
		},
	}
	if err := r.postJSON(ctx, base+"/search/structure", request, &start); err != nil {
		return nil, err
	}
	hash := firstNonEmpty([]string{start.Hash, start.Data.Hash})
	if hash == "" {
		return nil, nil
	}
	resultsURL := withQuery(base+"/search/"+url.PathEscape(hash)+"/results", map[string]string{
		"page":        "1",
		"max_results": fmt.Sprint(limit),
	})
	var payload any
	if err := r.getJSON(ctx, resultsURL, &payload); err != nil {
		return nil, err
	}
	hits := sureChEMBLDocumentHits(payload, limit)
	if len(hits) == 0 {
		hits = sureChEMBLStructureHits(payload, limit)
	}
	return hits, nil
}

func (r *Registry) searchClinicalTrials(ctx context.Context, query string, limit int) ([]Hit, error) {
	searchURL := withQuery(r.endpoints.ClinicalTrials, map[string]string{
		"query.term": query,
		"pageSize":   fmt.Sprint(limit),
	})
	var payload struct {
		Studies []struct {
			ProtocolSection struct {
				IdentificationModule struct {
					NCTID      string `json:"nctId"`
					BriefTitle string `json:"briefTitle"`
				} `json:"identificationModule"`
				StatusModule struct {
					OverallStatus string `json:"overallStatus"`
				} `json:"statusModule"`
			} `json:"protocolSection"`
		} `json:"studies"`
	}
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(payload.Studies))
	for idx, item := range payload.Studies {
		id := item.ProtocolSection.IdentificationModule.NCTID
		title := item.ProtocolSection.IdentificationModule.BriefTitle
		if title == "" {
			title = id
		}
		resultURL := "https://clinicaltrials.gov/study/" + id
		hits = append(hits, Hit{
			ID:         id,
			Source:     "ClinicalTrials.gov",
			Title:      title,
			Summary:    item.ProtocolSection.StatusModule.OverallStatus,
			URL:        resultURL,
			Score:      1 / float64(idx+1),
			Metadata:   map[string]string{"database": "clinicaltrials", "status": item.ProtocolSection.StatusModule.OverallStatus},
			Provenance: Provenance{Source: "ClinicalTrials.gov", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchNCBIGene(ctx context.Context, query string, limit int, organism string) ([]Hit, error) {
	base := strings.TrimRight(r.endpoints.NCBIGene, "/")
	term := query
	if organism != "" {
		term += " AND " + organism + "[orgn]"
	}
	searchURL := base + "/esearch.fcgi?db=gene&retmode=json&retmax=" + url.QueryEscape(fmt.Sprint(limit)) + "&term=" + url.QueryEscape(term)
	var search struct {
		Result struct {
			IDList []string `json:"idlist"`
		} `json:"esearchresult"`
	}
	if err := r.getJSON(ctx, searchURL, &search); err != nil {
		return nil, err
	}
	if len(search.Result.IDList) == 0 {
		return nil, nil
	}
	summaryURL := base + "/esummary.fcgi?db=gene&retmode=json&id=" + url.QueryEscape(strings.Join(search.Result.IDList, ","))
	var summary struct {
		Result map[string]any `json:"result"`
	}
	if err := r.getJSON(ctx, summaryURL, &summary); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(search.Result.IDList))
	for idx, id := range search.Result.IDList {
		item := mapFromAny(summary.Result[id])
		title := firstNonEmpty([]string{
			firstField(item, "nomenclaturesymbol", "name", "symbol"),
			"NCBI Gene " + id,
		})
		description := firstField(item, "description", "nomenclaturename")
		geneSummary := firstField(item, "summary")
		if geneSummary == "" {
			geneSummary = description
		}
		resultURL := "https://www.ncbi.nlm.nih.gov/gene/" + url.PathEscape(id)
		hits = append(hits, Hit{
			ID:      id,
			Source:  "NCBI Gene",
			Title:   title,
			Summary: geneSummary,
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database":    "ncbi_gene",
				"description": description,
				"chromosome":  firstField(item, "chromosome"),
				"maplocation": firstField(item, "maplocation"),
				"aliases":     firstField(item, "otheraliases"),
			},
			Provenance: Provenance{Source: "NCBI Gene", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchNCBIProtein(ctx context.Context, query string, limit int) ([]Hit, error) {
	base := strings.TrimRight(r.endpoints.NCBIProtein, "/")
	searchURL := base + "/esearch.fcgi?db=protein&retmode=json&retmax=" + url.QueryEscape(fmt.Sprint(limit)) + "&term=" + url.QueryEscape(query)
	var search struct {
		Result struct {
			IDList []string `json:"idlist"`
		} `json:"esearchresult"`
	}
	if err := r.getJSON(ctx, searchURL, &search); err != nil {
		return nil, err
	}
	if len(search.Result.IDList) == 0 {
		return nil, nil
	}
	summaryURL := base + "/esummary.fcgi?db=protein&retmode=json&id=" + url.QueryEscape(strings.Join(search.Result.IDList, ","))
	var summary struct {
		Result map[string]any `json:"result"`
	}
	if err := r.getJSON(ctx, summaryURL, &summary); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(search.Result.IDList))
	for idx, id := range search.Result.IDList {
		item := mapFromAny(summary.Result[id])
		accession := firstField(item, "accessionversion", "caption")
		title := firstNonEmpty([]string{firstField(item, "caption"), accession, "NCBI Protein " + id})
		resultURL := "https://www.ncbi.nlm.nih.gov/protein/" + url.PathEscape(firstNonEmpty([]string{accession, id}))
		proteinTitle := firstField(item, "title")
		hits = append(hits, Hit{
			ID:      firstNonEmpty([]string{accession, id}),
			Source:  "NCBI Protein",
			Title:   title,
			Summary: strings.TrimSpace(strings.Join(nonEmptyStrings([]string{proteinTitle, firstField(item, "organism")}), "; ")),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database":  "ncbi_protein",
				"uid":       id,
				"accession": accession,
				"organism":  firstField(item, "organism"),
				"length":    firstField(item, "slen"),
				"source":    firstField(item, "sourcedb"),
				"moltype":   firstField(item, "moltype"),
			},
			Provenance: Provenance{Source: "NCBI Protein", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchDataCite(ctx context.Context, query string, limit int) ([]Hit, error) {
	searchURL := withQuery(r.endpoints.DataCite, map[string]string{
		"query":      query,
		"page[size]": fmt.Sprint(limit),
		"sort":       "relevance",
	})
	var payload any
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	records := recordsFromAny(payload, "data")
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, record := range records {
		attrs := mapFromAny(record["attributes"])
		if len(attrs) == 0 {
			attrs = record
		}
		doi := firstNonEmpty([]string{firstField(attrs, "doi"), firstField(record, "id")})
		title := firstRecordField(attrs["titles"], "title")
		if title == "" {
			title = doi
		}
		description := firstRecordField(attrs["descriptions"], "description")
		resourceType := firstField(attrs, "resourceTypeGeneral", "resourceType")
		publisher := firstField(attrs, "publisher")
		year := firstField(attrs, "publicationYear", "published")
		creator := firstRecordField(attrs["creators"], "name")
		resultURL := firstField(attrs, "url")
		if resultURL == "" && doi != "" {
			resultURL = "https://doi.org/" + doi
		}
		if resultURL == "" {
			resultURL = "https://api.datacite.org/dois"
		}
		hits = append(hits, Hit{
			ID:      doi,
			Source:  "DataCite",
			Title:   title,
			Summary: cleanText(description),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database":  "datacite",
				"doi":       doi,
				"publisher": publisher,
				"year":      year,
				"type":      resourceType,
				"creator":   creator,
			},
			Provenance: Provenance{Source: "DataCite", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchZenodo(ctx context.Context, query string, limit int) ([]Hit, error) {
	searchURL := withQuery(r.endpoints.Zenodo, map[string]string{
		"q":    query,
		"size": fmt.Sprint(limit),
	})
	var payload any
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	root := mapFromAny(payload)
	hitsContainer := mapFromAny(root["hits"])
	records := recordsFromAny(hitsContainer["hits"], "hits")
	if len(records) == 0 {
		records = recordsFromAny(payload, "hits", "data")
	}
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, record := range records {
		metadata := mapFromAny(record["metadata"])
		id := firstField(record, "id", "conceptrecid")
		doi := firstNonEmpty([]string{firstField(record, "doi"), firstField(metadata, "doi")})
		title := firstNonEmpty([]string{firstField(metadata, "title"), firstField(record, "title"), id})
		resource := mapFromAny(metadata["resource_type"])
		resourceType := firstNonEmpty([]string{firstField(resource, "title"), firstField(resource, "type")})
		resultURL := firstField(record, "doi_url", "links.html", "html")
		if resultURL == "" && doi != "" {
			resultURL = "https://doi.org/" + doi
		}
		if resultURL == "" && id != "" {
			resultURL = "https://zenodo.org/records/" + url.PathEscape(id)
		}
		if resultURL == "" {
			resultURL = "https://zenodo.org"
		}
		hits = append(hits, Hit{
			ID:      firstNonEmpty([]string{doi, id}),
			Source:  "Zenodo",
			Title:   title,
			Summary: cleanText(firstField(metadata, "description")),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database":     "zenodo",
				"doi":          doi,
				"date":         firstField(metadata, "publication_date"),
				"type":         resourceType,
				"creator":      firstRecordField(metadata["creators"], "name"),
				"access_right": firstField(metadata, "access_right"),
			},
			Provenance: Provenance{Source: "Zenodo", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchNASAExoplanet(ctx context.Context, query string, limit int) ([]Hit, error) {
	term := strings.ToLower(strings.ReplaceAll(query, "'", "''"))
	adql := fmt.Sprintf("select top %d pl_name,hostname,disc_year,discoverymethod,sy_dist from ps where lower(pl_name) like '%%%s%%' or lower(hostname) like '%%%s%%'", limit, term, term)
	searchURL := withQuery(r.endpoints.NASAExoplanet, map[string]string{
		"query":  adql,
		"format": "json",
	})
	var payload any
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	records := recordsFromAny(payload, "data", "results")
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, record := range records {
		planet := firstField(record, "pl_name", "planet_name", "name")
		host := firstField(record, "hostname", "host")
		method := firstField(record, "discoverymethod", "discovery_method")
		year := firstField(record, "disc_year", "discovery_year")
		distance := firstField(record, "sy_dist", "distance")
		resultURL := "https://exoplanetarchive.ipac.caltech.edu/overview/" + url.PathEscape(planet)
		if planet == "" {
			resultURL = "https://exoplanetarchive.ipac.caltech.edu"
		}
		hits = append(hits, Hit{
			ID:      planet,
			Source:  "NASA Exoplanet Archive",
			Title:   firstNonEmpty([]string{planet, host}),
			Summary: strings.TrimSpace(strings.Join(nonEmptyStrings([]string{host, method, "discovered " + year, "distance " + distance + " pc"}), "; ")),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database": "nasa_exoplanet",
				"host":     host,
				"method":   method,
				"year":     year,
				"distance": distance,
			},
			Provenance: Provenance{Source: "NASA Exoplanet Archive", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchINSPIREHEP(ctx context.Context, query string, limit int) ([]Hit, error) {
	searchURL := withQuery(r.endpoints.INSPIREHEP, map[string]string{
		"q":    query,
		"size": fmt.Sprint(limit),
	})
	var payload any
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	root := mapFromAny(payload)
	hitsContainer := mapFromAny(root["hits"])
	records := recordsFromAny(hitsContainer["hits"], "hits")
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, record := range records {
		metadata := mapFromAny(record["metadata"])
		id := firstField(record, "id")
		title := firstRecordField(metadata["titles"], "title")
		if title == "" {
			title = id
		}
		doi := firstRecordField(metadata["dois"], "value")
		abstract := firstRecordField(metadata["abstracts"], "value")
		pubInfo := recordsFromAny(metadata["publication_info"])
		journal := ""
		year := ""
		if len(pubInfo) > 0 {
			journal = firstField(pubInfo[0], "journal_title", "journal")
			year = firstField(pubInfo[0], "year")
		}
		resultURL := "https://inspirehep.net/literature/" + url.PathEscape(id)
		hits = append(hits, Hit{
			ID:      id,
			Source:  "INSPIRE HEP",
			Title:   title,
			Summary: cleanText(abstract),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database":  "inspire_hep",
				"doi":       doi,
				"journal":   journal,
				"year":      year,
				"author":    firstRecordField(metadata["authors"], "full_name"),
				"citations": firstField(metadata, "citation_count"),
			},
			Provenance: Provenance{Source: "INSPIRE HEP", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchNOMADMaterials(ctx context.Context, query string, limit int) ([]Hit, error) {
	endpoint := strings.TrimRight(r.endpoints.NOMADMaterials, "/")
	if !strings.HasSuffix(endpoint, "/entries/query") {
		endpoint += "/entries/query"
	}
	elements := materialElements(query)
	if len(elements) == 0 {
		elements = []string{primarySearchTerm(query)}
	}
	request := map[string]any{
		"query": map[string]any{
			"results.material.elements": map[string]any{"all": elements},
		},
		"pagination": map[string]any{"page_size": limit},
		"required": map[string]any{"include": []string{
			"entry_id",
			"results.material.chemical_formula_hill",
			"results.material.elements",
			"results.method.method_name",
			"upload_create_time",
		}},
	}
	var payload any
	if err := r.postJSON(ctx, endpoint, request, &payload); err != nil {
		return nil, err
	}
	records := recordsFromAny(payload, "data", "results")
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, record := range records {
		id := firstField(record, "entry_id", "entryId", "id")
		formula := firstNonEmpty([]string{nestedField(record, "results.material.chemical_formula_hill"), id})
		elementsText := nestedField(record, "results.material.elements")
		method := nestedField(record, "results.method.method_name")
		created := firstField(record, "upload_create_time", "created")
		resultURL := "https://nomad-lab.eu/prod/v1/gui/search/entries/entry/id/" + url.PathEscape(id)
		if id == "" {
			resultURL = "https://nomad-lab.eu/prod/v1/gui/search/entries"
		}
		hits = append(hits, Hit{
			ID:      id,
			Source:  "NOMAD Materials",
			Title:   formula,
			Summary: strings.TrimSpace(strings.Join(nonEmptyStrings([]string{elementsText, method, created}), "; ")),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database": "nomad_materials",
				"formula":  formula,
				"elements": elementsText,
				"method":   method,
				"created":  created,
			},
			Provenance: Provenance{Source: "NOMAD Materials", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchHFRoboticsDatasets(ctx context.Context, query string, limit int) ([]Hit, error) {
	searchURL := withQuery(r.endpoints.HFRoboticsDatasets, map[string]string{
		"search":    query,
		"limit":     fmt.Sprint(limit),
		"sort":      "downloads",
		"direction": "-1",
	})
	var payload any
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	records := recordsFromAny(payload, "datasets", "data", "results", "items")
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, record := range records {
		id := firstField(record, "id", "datasetId", "name")
		if id == "" {
			continue
		}
		author := firstField(record, "author", "owner")
		downloads := firstField(record, "downloads")
		likes := firstField(record, "likes")
		tags := stringFromAny(record["tags"])
		modified := firstField(record, "lastModified", "last_modified", "updatedAt")
		resultURL := "https://huggingface.co/datasets/" + strings.TrimPrefix(id, "/")
		hits = append(hits, Hit{
			ID:      id,
			Source:  "Hugging Face Robotics Datasets",
			Title:   id,
			Summary: cleanText(firstField(record, "description", "summary", "cardData.description")),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database":      "hf_robotics_datasets",
				"author":        author,
				"downloads":     downloads,
				"likes":         likes,
				"tags":          tags,
				"last_modified": modified,
			},
			Provenance: Provenance{Source: "Hugging Face Hub", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchGitHubRobotics(ctx context.Context, query string, limit int) ([]Hit, error) {
	searchURL := withQuery(r.endpoints.GitHubRobotics, map[string]string{
		"q":        roboticsRepositoryQuery(query),
		"per_page": fmt.Sprint(limit),
		"sort":     "stars",
		"order":    "desc",
	})
	var payload any
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	records := recordsFromAny(payload, "items", "repositories", "data", "results")
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, record := range records {
		fullName := firstField(record, "full_name", "fullName", "name")
		id := firstNonEmpty([]string{fullName, firstField(record, "id")})
		if id == "" {
			continue
		}
		resultURL := firstField(record, "html_url", "url")
		if resultURL == "" && fullName != "" {
			resultURL = "https://github.com/" + fullName
		}
		hits = append(hits, Hit{
			ID:      id,
			Source:  "GitHub Robotics",
			Title:   firstNonEmpty([]string{fullName, id}),
			Summary: cleanText(firstField(record, "description", "summary")),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database": "github_robotics",
				"stars":    firstField(record, "stargazers_count", "stars"),
				"forks":    firstField(record, "forks_count", "forks"),
				"language": firstField(record, "language"),
				"topics":   stringFromAny(record["topics"]),
			},
			Provenance: Provenance{Source: "GitHub Search", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchROSPackages(ctx context.Context, query string, limit int) ([]Hit, error) {
	body, err := r.getText(ctx, r.endpoints.ROSPackages)
	if err != nil {
		return nil, err
	}
	return rosPackageHitsFromDistribution(body, query, limit), nil
}

func (r *Registry) searchOpenReviewEmbodied(ctx context.Context, query string, limit int) ([]Hit, error) {
	searchURL := withQuery(r.endpoints.OpenReviewEmbodied, map[string]string{
		"term":    query,
		"content": "all",
		"limit":   fmt.Sprint(limit),
	})
	var payload any
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	records := recordsFromAny(payload, "notes", "results", "data")
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, record := range records {
		content := mapFromAny(record["content"])
		id := firstField(record, "id", "forum")
		title := firstNonEmpty([]string{
			openReviewContentField(content, "title"),
			id,
		})
		abstract := firstNonEmpty([]string{
			openReviewContentField(content, "abstract"),
			openReviewContentField(content, "TL;DR"),
			openReviewContentField(content, "tl;dr"),
		})
		forum := firstNonEmpty([]string{firstField(record, "forum"), id})
		resultURL := "https://openreview.net/forum?id=" + url.QueryEscape(forum)
		if forum == "" {
			resultURL = "https://openreview.net"
		}
		hits = append(hits, Hit{
			ID:      id,
			Source:  "OpenReview Embodied AI",
			Title:   title,
			Summary: cleanText(abstract),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database":   "openreview_embodied",
				"invitation": firstField(record, "invitation"),
				"authors":    openReviewContentField(content, "authors"),
				"keywords":   openReviewContentField(content, "keywords"),
			},
			Provenance: Provenance{Source: "OpenReview", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchEnsembl(ctx context.Context, query string, limit int, organism string) ([]Hit, error) {
	species := ensemblSpecies(organism)
	symbol := primarySearchTerm(query)
	searchURL := strings.TrimRight(r.endpoints.Ensembl, "/") + "/xrefs/symbol/" + url.PathEscape(species) + "/" + url.PathEscape(symbol)
	var payload []struct {
		ID          string `json:"id"`
		DisplayID   string `json:"display_id"`
		Description string `json:"description"`
		Type        string `json:"type"`
		DBName      string `json:"dbname"`
	}
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	if len(payload) > limit {
		payload = payload[:limit]
	}
	hits := make([]Hit, 0, len(payload))
	for idx, item := range payload {
		title := firstNonEmpty([]string{item.DisplayID, item.ID})
		resultURL := "https://www.ensembl.org/" + ensemblURLSpecies(species) + "/Gene/Summary?g=" + url.QueryEscape(item.ID)
		hits = append(hits, Hit{
			ID:      item.ID,
			Source:  "Ensembl",
			Title:   title,
			Summary: item.Description,
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database": "ensembl",
				"species":  species,
				"type":     item.Type,
				"dbname":   item.DBName,
			},
			Provenance: Provenance{Source: "Ensembl", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchClinVar(ctx context.Context, query string, limit int) ([]Hit, error) {
	base := strings.TrimRight(r.endpoints.ClinVar, "/")
	searchURL := base + "/esearch.fcgi?db=clinvar&retmode=json&retmax=" + url.QueryEscape(fmt.Sprint(limit)) + "&term=" + url.QueryEscape(query)
	var search struct {
		Result struct {
			IDList []string `json:"idlist"`
		} `json:"esearchresult"`
	}
	if err := r.getJSON(ctx, searchURL, &search); err != nil {
		return nil, err
	}
	if len(search.Result.IDList) == 0 {
		return nil, nil
	}
	summaryURL := base + "/esummary.fcgi?db=clinvar&retmode=json&id=" + url.QueryEscape(strings.Join(search.Result.IDList, ","))
	var summary struct {
		Result map[string]any `json:"result"`
	}
	if err := r.getJSON(ctx, summaryURL, &summary); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(search.Result.IDList))
	for idx, id := range search.Result.IDList {
		item, _ := summary.Result[id].(map[string]any)
		title := firstNonEmpty([]string{
			stringFromAny(item["title"]),
			stringFromAny(item["variation_name"]),
			"ClinVar variation " + id,
		})
		significance := stringFromAny(item["clinical_significance"])
		accession := stringFromAny(item["accession"])
		resultURL := "https://www.ncbi.nlm.nih.gov/clinvar/variation/" + id + "/"
		hits = append(hits, Hit{
			ID:      id,
			Source:  "ClinVar",
			Title:   title,
			Summary: significance,
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database":     "clinvar",
				"accession":    accession,
				"significance": significance,
			},
			Provenance: Provenance{Source: "ClinVar", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchDbSNP(ctx context.Context, query string, limit int) ([]Hit, error) {
	base := strings.TrimRight(r.endpoints.DbSNP, "/")
	searchURL := base + "/esearch.fcgi?db=snp&retmode=json&retmax=" + url.QueryEscape(fmt.Sprint(limit)) + "&term=" + url.QueryEscape(query)
	var search struct {
		Result struct {
			IDList []string `json:"idlist"`
		} `json:"esearchresult"`
	}
	if err := r.getJSON(ctx, searchURL, &search); err != nil {
		return nil, err
	}
	if len(search.Result.IDList) == 0 {
		return nil, nil
	}
	summaryURL := base + "/esummary.fcgi?db=snp&retmode=json&id=" + url.QueryEscape(strings.Join(search.Result.IDList, ","))
	var summary struct {
		Result map[string]any `json:"result"`
	}
	if err := r.getJSON(ctx, summaryURL, &summary); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(search.Result.IDList))
	for idx, uid := range search.Result.IDList {
		item, _ := summary.Result[uid].(map[string]any)
		id := firstNonEmpty([]string{firstField(item, "refsnp_id", "snp_id", "rsid"), uid})
		if strings.HasPrefix(strings.ToLower(id), "rs") {
			id = strings.TrimPrefix(strings.ToLower(id), "rs")
		}
		title := "rs" + id
		chr := firstField(item, "chr", "chromosome")
		chrpos := firstField(item, "chrpos", "position", "chromosome_position")
		snpClass := firstField(item, "snp_class", "variant_type", "type")
		maf := dbSNPMafSummary(item["global_mafs"])
		summaryText := strings.TrimSpace(strings.Join(nonEmptyStrings([]string{snpClass, chrpos, maf}), "; "))
		resultURL := "https://www.ncbi.nlm.nih.gov/snp/" + title
		hits = append(hits, Hit{
			ID:      id,
			Source:  "dbSNP",
			Title:   title,
			Summary: summaryText,
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database": "dbsnp",
				"class":    snpClass,
				"chr":      chr,
				"chrpos":   chrpos,
				"maf":      maf,
			},
			Provenance: Provenance{Source: "dbSNP", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchGnomAD(ctx context.Context, query string, limit int) ([]Hit, error) {
	_ = limit
	var payload struct {
		Data struct {
			Variant *struct {
				VariantID       string `json:"variantId"`
				ReferenceGenome string `json:"reference_genome"`
				Chrom           string `json:"chrom"`
				Pos             any    `json:"pos"`
				Ref             string `json:"ref"`
				Alt             string `json:"alt"`
				Genome          struct {
					AC any `json:"ac"`
					AN any `json:"an"`
					AF any `json:"af"`
				} `json:"genome"`
				Exome struct {
					AC any `json:"ac"`
					AN any `json:"an"`
					AF any `json:"af"`
				} `json:"exome"`
				ColocatedVariants []string `json:"colocatedVariants"`
			} `json:"variant"`
			SearchResults []struct {
				Label string `json:"label"`
				Value string `json:"value"`
				URL   string `json:"url"`
			} `json:"searchResults"`
		} `json:"data"`
	}
	request := map[string]any{
		"query": `query SuperscienceVariant($variantId: String!, $dataset: DatasetId!, $search: String!) {
  variant(variantId: $variantId, dataset: $dataset) {
    variantId
    reference_genome
    chrom
    pos
    ref
    alt
    colocatedVariants
    genome { ac an af }
    exome { ac an af }
  }
  searchResults(dataset: $dataset, query: $search) {
    label
    value: url
  }
}`,
		"variables": map[string]any{
			"variantId": query,
			"dataset":   "gnomad_r4",
			"search":    query,
		},
	}
	if err := r.postJSON(ctx, r.endpoints.GnomAD, request, &payload); err != nil {
		return nil, err
	}
	if payload.Data.Variant != nil {
		variant := payload.Data.Variant
		title := firstNonEmpty([]string{variant.VariantID, query})
		genomeAF := stringFromAny(variant.Genome.AF)
		exomeAF := stringFromAny(variant.Exome.AF)
		resultURL := "https://gnomad.broadinstitute.org/variant/" + url.PathEscape(title) + "?dataset=gnomad_r4"
		hits := []Hit{{
			ID:     title,
			Source: "gnomAD",
			Title:  title,
			Summary: strings.TrimSpace(strings.Join(nonEmptyStrings([]string{
				variant.ReferenceGenome,
				variant.Chrom + ":" + stringFromAny(variant.Pos) + " " + variant.Ref + ">" + variant.Alt,
				"genome AF " + genomeAF,
				"exome AF " + exomeAF,
			}), "; ")),
			URL:   resultURL,
			Score: 1,
			Metadata: map[string]string{
				"database":  "gnomad",
				"reference": variant.ReferenceGenome,
				"chrom":     variant.Chrom,
				"pos":       stringFromAny(variant.Pos),
				"ref":       variant.Ref,
				"alt":       variant.Alt,
				"genome_af": genomeAF,
				"exome_af":  exomeAF,
			},
			Provenance: Provenance{Source: "gnomAD", URL: resultURL},
		}}
		return hits, nil
	}
	hits := make([]Hit, 0, len(payload.Data.SearchResults))
	for idx, item := range payload.Data.SearchResults {
		resultURL := item.Value
		if resultURL == "" {
			resultURL = item.URL
		}
		if resultURL != "" && strings.HasPrefix(resultURL, "/") {
			resultURL = "https://gnomad.broadinstitute.org" + resultURL
		}
		if resultURL == "" {
			resultURL = "https://gnomad.broadinstitute.org"
		}
		title := firstNonEmpty([]string{item.Label, resultURL})
		hits = append(hits, Hit{
			ID:         title,
			Source:     "gnomAD",
			Title:      title,
			Summary:    "gnomAD search result for " + query,
			URL:        resultURL,
			Score:      1 / float64(idx+1),
			Metadata:   map[string]string{"database": "gnomad"},
			Provenance: Provenance{Source: "gnomAD", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchMyGene(ctx context.Context, query string, limit int, organism string) ([]Hit, error) {
	params := map[string]string{
		"q":    query,
		"size": fmt.Sprint(limit),
	}
	if organism != "" {
		params["species"] = organism
	}
	searchURL := withQuery(r.endpoints.MyGene, params)
	var payload struct {
		Hits []struct {
			ID         string  `json:"_id"`
			Symbol     string  `json:"symbol"`
			Name       string  `json:"name"`
			TaxID      any     `json:"taxid"`
			EntrezGene any     `json:"entrezgene"`
			Score      float64 `json:"_score"`
		} `json:"hits"`
	}
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(payload.Hits))
	for idx, item := range payload.Hits {
		title := firstNonEmpty([]string{item.Symbol, item.ID})
		resultURL := "https://mygene.info/v3/gene/" + item.ID
		taxID := stringFromAny(item.TaxID)
		entrez := stringFromAny(item.EntrezGene)
		hits = append(hits, Hit{
			ID:      item.ID,
			Source:  "MyGene.info",
			Title:   title,
			Summary: item.Name,
			URL:     resultURL,
			Score:   nonZeroScore(item.Score, idx),
			Metadata: map[string]string{
				"database": "mygene",
				"taxid":    taxID,
				"entrez":   entrez,
			},
			Provenance: Provenance{Source: "MyGene.info", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchMyVariant(ctx context.Context, query string, limit int) ([]Hit, error) {
	searchURL := withQuery(r.endpoints.MyVariant, map[string]string{
		"q":      query,
		"size":   fmt.Sprint(limit),
		"fields": "all",
	})
	var payload any
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	records := recordsFromAny(payload, "hits", "results", "data")
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, item := range records {
		id := firstField(item, "_id", "id", "hgvs")
		rsid := firstNonEmpty([]string{
			nestedField(item, "dbsnp.rsid"),
			firstField(item, "rsid"),
		})
		title := firstNonEmpty([]string{rsid, id})
		condition := firstNonEmpty([]string{
			nestedField(item, "clinvar.rcv.conditions.name"),
			nestedField(item, "clinvar.rcv.condition"),
			nestedField(item, "clinvar.trait"),
		})
		cadd := nestedField(item, "cadd.phred")
		resultURL := "https://myvariant.info/v1/variant/" + url.PathEscape(id)
		if id == "" {
			resultURL = "https://myvariant.info"
		}
		hits = append(hits, Hit{
			ID:      id,
			Source:  "MyVariant.info",
			Title:   title,
			Summary: strings.TrimSpace(strings.Join(nonEmptyStrings([]string{condition, "CADD " + cadd}), "; ")),
			URL:     resultURL,
			Score:   nonZeroScore(floatFromAny(item["_score"]), idx),
			Metadata: map[string]string{
				"database":  "myvariant",
				"rsid":      rsid,
				"condition": condition,
				"cadd":      cadd,
			},
			Provenance: Provenance{Source: "MyVariant.info", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchUCSC(ctx context.Context, query string, limit int, organism string) ([]Hit, error) {
	endpoint := strings.TrimRight(r.endpoints.UCSC, "/")
	if !strings.HasSuffix(endpoint, "/search") {
		endpoint += "/search"
	}
	genome := ucscGenome(organism)
	searchURL := withQuery(endpoint, map[string]string{
		"genome": genome,
		"search": query,
	})
	var payload struct {
		PositionMatches []struct {
			Position    string `json:"position"`
			Description string `json:"description"`
			Genome      string `json:"genome"`
		} `json:"positionMatches"`
		TrackMatches []struct {
			Track       string `json:"track"`
			ShortLabel  string `json:"shortLabel"`
			LongLabel   string `json:"longLabel"`
			Description string `json:"description"`
		} `json:"trackMatches"`
	}
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, limit)
	for idx, item := range payload.PositionMatches {
		if len(hits) >= limit {
			break
		}
		itemGenome := firstNonEmpty([]string{item.Genome, genome})
		resultURL := withQuery("https://genome.ucsc.edu/cgi-bin/hgTracks", map[string]string{
			"db":       itemGenome,
			"position": item.Position,
		})
		hits = append(hits, Hit{
			ID:      item.Position,
			Source:  "UCSC Genome Browser",
			Title:   item.Position,
			Summary: item.Description,
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database": "ucsc",
				"genome":   itemGenome,
				"type":     "position",
			},
			Provenance: Provenance{Source: "UCSC Genome Browser", URL: resultURL},
		})
	}
	if len(hits) > 0 {
		return hits, nil
	}
	for idx, item := range payload.TrackMatches {
		if len(hits) >= limit {
			break
		}
		title := firstNonEmpty([]string{item.ShortLabel, item.LongLabel, item.Track})
		resultURL := withQuery("https://genome.ucsc.edu/cgi-bin/hgTracks", map[string]string{
			"db": genome,
		})
		hits = append(hits, Hit{
			ID:      item.Track,
			Source:  "UCSC Genome Browser",
			Title:   title,
			Summary: firstNonEmpty([]string{item.Description, item.LongLabel}),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database": "ucsc",
				"genome":   genome,
				"type":     "track",
				"track":    item.Track,
			},
			Provenance: Provenance{Source: "UCSC Genome Browser", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchGEO(ctx context.Context, query string, limit int) ([]Hit, error) {
	base := strings.TrimRight(r.endpoints.GEO, "/")
	searchURL := base + "/esearch.fcgi?db=gds&retmode=json&retmax=" + url.QueryEscape(fmt.Sprint(limit)) + "&term=" + url.QueryEscape(query)
	var search struct {
		Result struct {
			IDList []string `json:"idlist"`
		} `json:"esearchresult"`
	}
	if err := r.getJSON(ctx, searchURL, &search); err != nil {
		return nil, err
	}
	if len(search.Result.IDList) == 0 {
		return nil, nil
	}
	summaryURL := base + "/esummary.fcgi?db=gds&retmode=json&id=" + url.QueryEscape(strings.Join(search.Result.IDList, ","))
	var summary struct {
		Result map[string]any `json:"result"`
	}
	if err := r.getJSON(ctx, summaryURL, &summary); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(search.Result.IDList))
	for idx, uid := range search.Result.IDList {
		item, _ := summary.Result[uid].(map[string]any)
		accession := firstField(item, "accession", "Accession", "gse", "gds")
		title := firstNonEmpty([]string{firstField(item, "title", "Title"), accession, "GEO dataset " + uid})
		summaryText := firstNonEmpty([]string{firstField(item, "summary", "description"), firstField(item, "gdsType", "gdstype")})
		resultURL := "https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=" + url.QueryEscape(accession)
		if accession == "" {
			resultURL = "https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi"
		}
		hits = append(hits, Hit{
			ID:      firstNonEmpty([]string{accession, uid}),
			Source:  "NCBI GEO",
			Title:   title,
			Summary: summaryText,
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database":  "geo",
				"accession": accession,
				"type":      firstField(item, "gdsType", "gdstype"),
				"taxon":     firstField(item, "taxon", "taxon_name"),
			},
			Provenance: Provenance{Source: "NCBI GEO", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchGTEx(ctx context.Context, query string, limit int) ([]Hit, error) {
	base := strings.TrimRight(r.endpoints.GTEx, "/")
	searchURL := withQuery(base+"/reference/geneSearch", map[string]string{
		"geneId":       query,
		"itemsPerPage": "1",
	})
	var genes any
	if err := r.getJSON(ctx, searchURL, &genes); err != nil {
		return nil, err
	}
	geneRecords := recordsFromAny(genes, "data", "results")
	if len(geneRecords) == 0 {
		return nil, nil
	}
	gene := geneRecords[0]
	gencodeID := firstField(gene, "gencodeId", "gencode_id", "geneId")
	geneSymbol := firstNonEmpty([]string{firstField(gene, "geneSymbol", "symbol"), query})
	if gencodeID == "" {
		return nil, nil
	}
	expressionURL := withQuery(base+"/expression/medianGeneExpression", map[string]string{
		"gencodeId":    gencodeID,
		"itemsPerPage": fmt.Sprint(limit),
	})
	var expression any
	if err := r.getJSON(ctx, expressionURL, &expression); err != nil {
		return nil, err
	}
	records := recordsFromAny(expression, "data", "results")
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, item := range records {
		tissue := firstField(item, "tissueSiteDetailId", "tissueSiteDetail", "tissue")
		median := firstField(item, "median", "medianExpression")
		unit := firstField(item, "unit", "units")
		title := strings.TrimSpace(geneSymbol + " in " + tissue)
		if tissue == "" {
			title = geneSymbol
		}
		resultURL := "https://gtexportal.org/home/gene/" + url.PathEscape(geneSymbol)
		hits = append(hits, Hit{
			ID:      gencodeID,
			Source:  "GTEx",
			Title:   title,
			Summary: strings.TrimSpace(strings.Join(nonEmptyStrings([]string{"median " + median, unit}), " ")),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database": "gtex",
				"gene":     geneSymbol,
				"gencode":  gencodeID,
				"tissue":   tissue,
				"median":   median,
				"unit":     unit,
			},
			Provenance: Provenance{Source: "GTEx", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchHPA(ctx context.Context, query string, limit int) ([]Hit, error) {
	searchURL := withQuery(r.endpoints.HPA, map[string]string{
		"search":   query,
		"format":   "json",
		"compress": "no",
	})
	var payload any
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	records := recordsFromAny(payload, "results", "data")
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, item := range records {
		gene := firstField(item, "Gene name", "Gene", "gene", "symbol")
		ensembl := firstField(item, "Gene", "Ensembl", "ensembl")
		proteinClass := firstField(item, "Protein class", "protein_class")
		tissueSpecificity := firstField(item, "RNA tissue specificity", "rna_tissue_specificity")
		resultURL := "https://www.proteinatlas.org/" + url.PathEscape(ensembl) + "-" + url.PathEscape(gene)
		if ensembl == "" {
			resultURL = "https://www.proteinatlas.org/search/" + url.QueryEscape(query)
		}
		hits = append(hits, Hit{
			ID:      firstNonEmpty([]string{ensembl, gene}),
			Source:  "Human Protein Atlas",
			Title:   firstNonEmpty([]string{gene, ensembl}),
			Summary: strings.TrimSpace(strings.Join(nonEmptyStrings([]string{proteinClass, tissueSpecificity}), "; ")),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database":           "hpa",
				"gene":               gene,
				"ensembl":            ensembl,
				"uniprot":            firstField(item, "Uniprot", "uniprot"),
				"protein_class":      proteinClass,
				"tissue_specificity": tissueSpecificity,
			},
			Provenance: Provenance{Source: "Human Protein Atlas", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchDepMap(ctx context.Context, query string, limit int) ([]Hit, error) {
	filter := `[{"name":"symbol","op":"ilike","val":"` + strings.ReplaceAll(query, `"`, `\"`) + `"}]`
	searchURL := withQuery(strings.TrimRight(r.endpoints.DepMap, "/")+"/genes", map[string]string{
		"filter":     filter,
		"page[size]": fmt.Sprint(limit),
	})
	var payload any
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	records := recordsFromAny(payload, "data", "results")
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, record := range records {
		attrs := mapFromAny(record["attributes"])
		if len(attrs) == 0 {
			attrs = record
		}
		id := firstNonEmpty([]string{firstField(record, "id"), firstField(attrs, "symbol", "hgnc_id")})
		symbol := firstNonEmpty([]string{firstField(attrs, "symbol"), id})
		action := firstField(attrs, "method_of_action", "methodOfAction")
		driver := firstField(attrs, "cancer_driver", "cancerDriver")
		resultURL := "https://depmap.sanger.ac.uk/gene/" + url.PathEscape(symbol)
		hits = append(hits, Hit{
			ID:      id,
			Source:  "DepMap Sanger",
			Title:   symbol,
			Summary: strings.TrimSpace(strings.Join(nonEmptyStrings([]string{action, "cancer driver " + driver}), "; ")),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database":         "depmap",
				"hgnc":             firstField(attrs, "hgnc_id", "hgnc"),
				"location":         firstField(attrs, "location"),
				"method_of_action": action,
				"cancer_driver":    driver,
			},
			Provenance: Provenance{Source: "DepMap Sanger", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchExpressionAtlas(ctx context.Context, query string, limit int) ([]Hit, error) {
	return r.searchAtlasExperiments(ctx, r.endpoints.ExpressionAtlas, "Expression Atlas", "expression_atlas", query, limit)
}

func (r *Registry) searchSingleCellAtlas(ctx context.Context, query string, limit int) ([]Hit, error) {
	return r.searchAtlasExperiments(ctx, r.endpoints.SingleCellAtlas, "Single Cell Expression Atlas", "single_cell_atlas", query, limit)
}

func (r *Registry) searchAtlasExperiments(ctx context.Context, endpoint string, source string, database string, query string, limit int) ([]Hit, error) {
	searchURL := withQuery(endpoint, map[string]string{
		"keywords": query,
	})
	var payload struct {
		Experiments []struct {
			Accession      string   `json:"experimentAccession"`
			Description    string   `json:"experimentDescription"`
			Species        string   `json:"species"`
			ExperimentType string   `json:"experimentType"`
			TechnologyType []string `json:"technologyType"`
			NumberOfAssays any      `json:"numberOfAssays"`
			LastUpdate     string   `json:"lastUpdate"`
		} `json:"experiments"`
	}
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	if len(payload.Experiments) > limit {
		payload.Experiments = payload.Experiments[:limit]
	}
	hits := make([]Hit, 0, len(payload.Experiments))
	for idx, item := range payload.Experiments {
		basePath := "/gxa/experiments/"
		if database == "single_cell_atlas" {
			basePath = "/gxa/sc/experiments/"
		}
		resultURL := "https://www.ebi.ac.uk" + basePath + url.PathEscape(item.Accession)
		hits = append(hits, Hit{
			ID:      item.Accession,
			Source:  source,
			Title:   firstNonEmpty([]string{item.Description, item.Accession}),
			Summary: strings.TrimSpace(strings.Join(nonEmptyStrings([]string{item.Species, item.ExperimentType, strings.Join(item.TechnologyType, ", ")}), "; ")),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database":   database,
				"species":    item.Species,
				"type":       item.ExperimentType,
				"technology": strings.Join(item.TechnologyType, ", "),
				"assays":     stringFromAny(item.NumberOfAssays),
				"updated":    item.LastUpdate,
			},
			Provenance: Provenance{Source: source, URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchReactome(ctx context.Context, query string, limit int, organism string) ([]Hit, error) {
	endpoint := strings.TrimRight(r.endpoints.Reactome, "/")
	if !strings.HasSuffix(endpoint, "/search/query") {
		endpoint += "/search/query"
	}
	params := map[string]string{
		"query":    query,
		"pageSize": fmt.Sprint(limit),
	}
	if organism != "" {
		params["species"] = organism
	}
	searchURL := withQuery(endpoint, params)
	var payload struct {
		Results []struct {
			STID        string `json:"stId"`
			DBID        any    `json:"dbId"`
			Name        string `json:"name"`
			Summation   string `json:"summation"`
			SpeciesName string `json:"speciesName"`
			Type        string `json:"type"`
		} `json:"results"`
	}
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(payload.Results))
	for idx, item := range payload.Results {
		id := firstNonEmpty([]string{item.STID, stringFromAny(item.DBID)})
		title := firstNonEmpty([]string{item.Name, id})
		resultURL := "https://reactome.org/content/detail/" + id
		hits = append(hits, Hit{
			ID:      id,
			Source:  "Reactome",
			Title:   title,
			Summary: item.Summation,
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database": "reactome",
				"species":  item.SpeciesName,
				"type":     item.Type,
			},
			Provenance: Provenance{Source: "Reactome", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchSTRING(ctx context.Context, query string, limit int, organism string) ([]Hit, error) {
	searchURL := withQuery(r.endpoints.STRING, map[string]string{
		"identifiers":     primarySearchTerm(query),
		"species":         stringTaxonID(organism),
		"limit":           fmt.Sprint(limit),
		"caller_identity": "superscience",
	})
	var payload []struct {
		StringIDA      string  `json:"stringId_A"`
		PreferredNameA string  `json:"preferredName_A"`
		StringIDB      string  `json:"stringId_B"`
		PreferredNameB string  `json:"preferredName_B"`
		Annotation     string  `json:"annotation"`
		Score          float64 `json:"score"`
		TaxonID        any     `json:"ncbiTaxonId"`
	}
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	if len(payload) > limit {
		payload = payload[:limit]
	}
	hits := make([]Hit, 0, len(payload))
	for idx, item := range payload {
		id := firstNonEmpty([]string{item.StringIDA + ":" + item.StringIDB, item.StringIDA, item.StringIDB})
		titleA := firstNonEmpty([]string{item.PreferredNameA, item.StringIDA})
		titleB := firstNonEmpty([]string{item.PreferredNameB, item.StringIDB})
		title := strings.Trim(strings.TrimSpace(titleA)+"-"+strings.TrimSpace(titleB), "-")
		resultURL := "https://string-db.org/network/" + url.PathEscape(firstNonEmpty([]string{item.StringIDA, item.PreferredNameA, query}))
		hits = append(hits, Hit{
			ID:      id,
			Source:  "STRING",
			Title:   title,
			Summary: item.Annotation,
			URL:     resultURL,
			Score:   nonZeroScore(item.Score, idx),
			Metadata: map[string]string{
				"database": "string",
				"taxid":    stringFromAny(item.TaxonID),
			},
			Provenance: Provenance{Source: "STRING", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchBioGRID(ctx context.Context, query string, limit int) ([]Hit, error) {
	endpoint := strings.TrimRight(r.endpoints.BioGRID, "/")
	accessKey := firstNonEmpty([]string{os.Getenv("BIOGRID_ACCESS_KEY"), os.Getenv("BIOGRID_ACCESSKEY")})
	if accessKey == "" && strings.Contains(endpoint, "thebiogrid.org") {
		return nil, nil
	}
	params := map[string]string{
		"searchNames":                   "true",
		"geneList":                      primarySearchTerm(query),
		"includeInteractors":            "true",
		"includeInteractorInteractions": "false",
		"taxId":                         "9606",
		"format":                        "json",
		"max":                           fmt.Sprint(limit),
	}
	if accessKey != "" {
		params["accesskey"] = accessKey
	}
	searchURL := withQuery(endpoint, params)
	var payload any
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	records := recordsFromAny(payload, "data", "results", "interactions")
	if len(records) == 0 {
		records = recordsFromObjectValues(payload)
	}
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, item := range records {
		id := firstField(item, "BIOGRID_INTERACTION_ID", "biogrid_interaction_id", "interaction_id", "id")
		a := firstField(item, "OFFICIAL_SYMBOL_A", "official_symbol_a", "geneA", "symbolA", "interactorA")
		b := firstField(item, "OFFICIAL_SYMBOL_B", "official_symbol_b", "geneB", "symbolB", "interactorB")
		title := strings.Trim(strings.TrimSpace(a)+"-"+strings.TrimSpace(b), "-")
		if title == "" {
			title = "BioGRID interaction " + id
		}
		system := firstField(item, "EXPERIMENTAL_SYSTEM", "experimental_system", "experimentalSystem", "system")
		throughput := firstField(item, "THROUGHPUT", "throughput")
		pubmed := firstField(item, "PUBMED_ID", "pubmed_id", "pubmed", "publication")
		resultURL := "https://thebiogrid.org/" + url.PathEscape(id)
		if id == "" {
			resultURL = "https://thebiogrid.org"
		}
		hits = append(hits, Hit{
			ID:      id,
			Source:  "BioGRID",
			Title:   title,
			Summary: strings.TrimSpace(strings.Join(nonEmptyStrings([]string{system, throughput, "PMID " + pubmed}), "; ")),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database":   "biogrid",
				"interactor": b,
				"system":     system,
				"throughput": throughput,
				"pubmed":     pubmed,
			},
			Provenance: Provenance{Source: "BioGRID", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchIntAct(ctx context.Context, query string, limit int) ([]Hit, error) {
	endpoint := strings.TrimRight(r.endpoints.IntAct, "/")
	var searchURL string
	if strings.HasSuffix(endpoint, "/findInteractor") {
		searchURL = endpoint + "/" + url.PathEscape(primarySearchTerm(query))
	} else {
		searchURL = withQuery(endpoint, map[string]string{"query": query, "pageSize": fmt.Sprint(limit), "format": "json"})
	}
	var payload any
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	records := recordsFromAny(payload, "data", "results", "interactions", "content")
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, item := range records {
		id := firstNonEmpty([]string{
			firstField(item, "interactionAc", "interaction_ac", "interactionAcs", "ac", "id"),
			firstField(item, "interactionIdentifier"),
		})
		a := firstField(item, "moleculeA", "interactorA", "interactorAName", "preferredNameA", "molecule_a")
		b := firstField(item, "moleculeB", "interactorB", "interactorBName", "preferredNameB", "molecule_b")
		title := strings.Trim(strings.TrimSpace(a)+"-"+strings.TrimSpace(b), "-")
		if title == "" {
			title = firstNonEmpty([]string{firstField(item, "name", "label"), "IntAct interaction " + id})
		}
		interactionType := firstField(item, "interactionType", "type", "interaction_type")
		pubmed := firstField(item, "publicationPubmedIdentifier", "pubmed", "publication", "pmid")
		resultURL := "https://www.ebi.ac.uk/intact/details/interaction/" + url.PathEscape(id)
		if id == "" {
			resultURL = "https://www.ebi.ac.uk/intact"
		}
		hits = append(hits, Hit{
			ID:      id,
			Source:  "IntAct",
			Title:   title,
			Summary: strings.TrimSpace(strings.Join(nonEmptyStrings([]string{interactionType, "PMID " + pubmed}), "; ")),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database": "intact",
				"type":     interactionType,
				"pubmed":   pubmed,
			},
			Provenance: Provenance{Source: "IntAct", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchKEGG(ctx context.Context, query string, limit int) ([]Hit, error) {
	searchURL := strings.TrimRight(r.endpoints.KEGG, "/") + "/find/pathway/" + url.PathEscape(primarySearchTerm(query))
	body, err := r.getText(ctx, searchURL)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(body, "\n")
	hits := make([]Hit, 0, limit)
	for idx, line := range lines {
		if len(hits) >= limit {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		entry := strings.TrimSpace(parts[0])
		title := entry
		if len(parts) > 1 {
			title = strings.TrimSpace(parts[1])
		}
		id := strings.TrimPrefix(entry, "path:")
		resultURL := "https://www.kegg.jp/entry/" + url.PathEscape(id)
		hits = append(hits, Hit{
			ID:      id,
			Source:  "KEGG",
			Title:   title,
			Summary: "KEGG pathway match for " + query,
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database": "kegg",
				"entry":    entry,
				"type":     "pathway",
			},
			Provenance: Provenance{Source: "KEGG", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchOpenTargets(ctx context.Context, query string, limit int) ([]Hit, error) {
	request := map[string]any{
		"query": `query SuperscienceSearch($queryString: String!) {
  search(queryString: $queryString) {
    hits {
      id
      entity
      name
      description
    }
  }
}`,
		"variables": map[string]any{"queryString": query},
	}
	var payload struct {
		Data struct {
			Search struct {
				Hits []struct {
					ID          string `json:"id"`
					Entity      string `json:"entity"`
					Name        string `json:"name"`
					Description string `json:"description"`
				} `json:"hits"`
			} `json:"search"`
		} `json:"data"`
	}
	if err := r.postJSON(ctx, r.endpoints.OpenTargets, request, &payload); err != nil {
		return nil, err
	}
	if len(payload.Data.Search.Hits) > limit {
		payload.Data.Search.Hits = payload.Data.Search.Hits[:limit]
	}
	hits := make([]Hit, 0, len(payload.Data.Search.Hits))
	for idx, item := range payload.Data.Search.Hits {
		title := firstNonEmpty([]string{item.Name, item.ID})
		resultURL := openTargetsURL(item.Entity, item.ID)
		hits = append(hits, Hit{
			ID:      item.ID,
			Source:  "Open Targets",
			Title:   title,
			Summary: item.Description,
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database": "opentargets",
				"entity":   item.Entity,
			},
			Provenance: Provenance{Source: "Open Targets", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchWikiPathways(ctx context.Context, query string, limit int) ([]Hit, error) {
	endpoint := strings.TrimRight(r.endpoints.WikiPathways, "/")
	if !strings.HasSuffix(endpoint, "/findPathwaysByText") {
		endpoint += "/findPathwaysByText"
	}
	searchURL := withQuery(endpoint, map[string]string{"query": query, "format": "json"})
	var payload any
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	records := recordsFromAny(payload, "result", "results", "pathways", "data")
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, item := range records {
		id := firstField(item, "id", "identifier", "wpid", "pathway")
		title := firstNonEmpty([]string{firstField(item, "name", "title"), id})
		species := firstField(item, "species", "organism")
		resultURL := firstField(item, "url", "uri")
		if resultURL == "" && id != "" {
			resultURL = "https://www.wikipathways.org/pathways/" + url.PathEscape(id) + ".html"
		}
		if resultURL == "" {
			resultURL = "https://www.wikipathways.org"
		}
		hits = append(hits, Hit{
			ID:      id,
			Source:  "WikiPathways",
			Title:   title,
			Summary: species,
			URL:     resultURL,
			Score:   nonZeroScore(floatFromAny(item["score"]), idx),
			Metadata: map[string]string{
				"database": "wikipathways",
				"species":  species,
			},
			Provenance: Provenance{Source: "WikiPathways", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchInterPro(ctx context.Context, query string, limit int) ([]Hit, error) {
	searchURL := withQuery(r.endpoints.InterPro, map[string]string{
		"search":    query,
		"page_size": fmt.Sprint(limit),
	})
	var payload any
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	records := recordsFromAny(payload, "results", "data")
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, record := range records {
		metadata := mapFromAny(record["metadata"])
		if len(metadata) == 0 {
			metadata = record
		}
		id := firstField(metadata, "accession", "id")
		title := firstNonEmpty([]string{firstField(metadata, "name", "short_name"), id})
		entryType := firstField(metadata, "type", "entry_type")
		sourceDB := firstField(metadata, "source_database", "sourceDatabase")
		resultURL := "https://www.ebi.ac.uk/interpro/entry/InterPro/" + url.PathEscape(id) + "/"
		if id == "" {
			resultURL = "https://www.ebi.ac.uk/interpro/"
		}
		hits = append(hits, Hit{
			ID:      id,
			Source:  "InterPro",
			Title:   title,
			Summary: strings.TrimSpace(strings.Join(nonEmptyStrings([]string{entryType, sourceDB, "proteins " + firstField(record, "proteins_count")}), "; ")),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database": "interpro",
				"type":     entryType,
				"source":   sourceDB,
			},
			Provenance: Provenance{Source: "InterPro", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchPDBe(ctx context.Context, query string, limit int) ([]Hit, error) {
	searchURL := withQuery(r.endpoints.PDBe, map[string]string{
		"q":    query,
		"wt":   "json",
		"rows": fmt.Sprint(limit),
	})
	var payload any
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	root := mapFromAny(payload)
	response := mapFromAny(root["response"])
	records := recordsFromAny(response["docs"], "docs")
	if len(records) == 0 {
		records = recordsFromAny(payload, "results", "data", "docs")
	}
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, item := range records {
		id := strings.ToUpper(firstField(item, "pdb_id", "pdbId", "entry_id", "identifier", "id"))
		title := firstNonEmpty([]string{firstField(item, "title", "citation_title", "structure_title", "t_molecule_info"), "PDBe structure " + id})
		method := firstField(item, "experimental_method", "experimentalMethod", "experiment_type")
		resolution := firstField(item, "resolution", "resolution_combined")
		resultURL := "https://www.ebi.ac.uk/pdbe/entry/pdb/" + strings.ToLower(url.PathEscape(id))
		if id == "" {
			resultURL = "https://www.ebi.ac.uk/pdbe/"
		}
		hits = append(hits, Hit{
			ID:      id,
			Source:  "PDBe",
			Title:   title,
			Summary: strings.TrimSpace(strings.Join(nonEmptyStrings([]string{method, "resolution " + resolution}), "; ")),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database":   "pdbe",
				"method":     method,
				"resolution": resolution,
			},
			Provenance: Provenance{Source: "PDBe", URL: resultURL},
		})
	}
	return hits, nil
}

func (r *Registry) searchSIFTS(ctx context.Context, query string, limit int) ([]Hit, error) {
	accession := primarySearchTerm(query)
	searchURL := strings.TrimRight(r.endpoints.SIFTS, "/") + "/" + url.PathEscape(accession)
	var payload any
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	hits := siftsBestStructureHits(payload, accession, limit)
	if len(hits) > 0 {
		return hits, nil
	}
	return siftsMappingHits(payload, accession, limit), nil
}

func (r *Registry) searchBioRxiv(ctx context.Context, query string, limit int) ([]Hit, error) {
	searchURL := strings.TrimRight(r.endpoints.BioRxiv, "/") + "/30d/0/json"
	var payload struct {
		Collection []struct {
			DOI      string `json:"doi"`
			Title    string `json:"title"`
			Authors  string `json:"authors"`
			Abstract string `json:"abstract"`
			Date     string `json:"date"`
			Category string `json:"category"`
			Server   string `json:"server"`
		} `json:"collection"`
	}
	if err := r.getJSON(ctx, searchURL, &payload); err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	hits := make([]Hit, 0, limit)
	for idx, item := range payload.Collection {
		haystack := strings.ToLower(strings.Join([]string{item.DOI, item.Title, item.Authors, item.Abstract, item.Category}, " "))
		if query != "" && !strings.Contains(haystack, query) {
			continue
		}
		resultURL := "https://www.biorxiv.org/content/" + item.DOI + "v1"
		if item.DOI == "" {
			resultURL = "https://www.biorxiv.org"
		}
		hits = append(hits, Hit{
			ID:      item.DOI,
			Source:  "bioRxiv",
			Title:   firstNonEmpty([]string{item.Title, item.DOI}),
			Summary: item.Abstract,
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database": "biorxiv",
				"date":     item.Date,
				"category": item.Category,
				"server":   item.Server,
			},
			Provenance: Provenance{Source: "bioRxiv", URL: resultURL},
		})
		if len(hits) >= limit {
			break
		}
	}
	return hits, nil
}

func (r *Registry) getJSON(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return errors.New(resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (r *Registry) postJSON(ctx context.Context, endpoint string, input any, out any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return errors.New(resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (r *Registry) getText(ctx context.Context, endpoint string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", errors.New(resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (r *Registry) database(id string) (Database, bool) {
	for _, db := range r.dbs {
		if db.ID == id {
			return db, true
		}
	}
	return Database{}, false
}

func withQuery(endpoint string, params map[string]string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	query := parsed.Query()
	for key, value := range params {
		if value != "" {
			query.Set(key, value)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func abstractFromInvertedIndex(index map[string][]int) string {
	if len(index) == 0 {
		return ""
	}
	positions := map[int]string{}
	maxPosition := 0
	for word, wordPositions := range index {
		for _, position := range wordPositions {
			positions[position] = word
			if position > maxPosition {
				maxPosition = position
			}
		}
	}
	words := make([]string, 0, maxPosition+1)
	for position := 0; position <= maxPosition; position++ {
		word := positions[position]
		if word != "" {
			words = append(words, word)
		}
	}
	return strings.Join(words, " ")
}

func firstNonEmpty(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func europePMCURL(id string) string {
	if strings.HasPrefix(strings.ToUpper(id), "PMC") {
		return "https://europepmc.org/article/PMC/" + id
	}
	if id == "" {
		return "https://europepmc.org"
	}
	return "https://europepmc.org/article/MED/" + id
}

func openTargetsURL(entity string, id string) string {
	entity = strings.TrimSpace(strings.ToLower(entity))
	id = strings.TrimSpace(id)
	if id == "" {
		return "https://platform.opentargets.org"
	}
	switch entity {
	case "target", "disease", "drug", "variant", "study", "credible_set", "credibleset":
		return "https://platform.opentargets.org/" + strings.ReplaceAll(entity, "_", "-") + "/" + url.PathEscape(id)
	default:
		return "https://platform.opentargets.org/search?q=" + url.QueryEscape(id)
	}
}

func nonZeroScore(score float64, idx int) float64 {
	if score > 0 {
		return score
	}
	return 1 / float64(idx+1)
}

func primarySearchTerm(query string) string {
	fields := strings.Fields(strings.TrimSpace(query))
	if len(fields) == 0 {
		return query
	}
	return strings.Trim(fields[0], " ,;:")
}

func roboticsRepositoryQuery(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return "robotics OR robot OR embodied-ai"
	}
	lower := strings.ToLower(query)
	for _, marker := range []string{"robot", "robotics", "embodied", "ros", "lerobot", "maniskill", "mujoco"} {
		if strings.Contains(lower, marker) {
			return query
		}
	}
	return query + " robotics OR robot OR embodied-ai"
}

func rosPackageHitsFromDistribution(body string, query string, limit int) []Hit {
	query = strings.ToLower(strings.TrimSpace(query))
	hits := make([]Hit, 0, limit)
	repo := ""
	section := ""
	status := ""
	sourceURL := ""
	releaseURL := ""
	inPackages := false
	for _, line := range strings.Split(body, "\n") {
		if len(hits) >= limit {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || trimmed == "repositories:" {
			continue
		}
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(trimmed, ":") {
			repo = strings.TrimSuffix(trimmed, ":")
			section = ""
			status = ""
			sourceURL = ""
			releaseURL = ""
			inPackages = false
			continue
		}
		if repo == "" {
			continue
		}
		if strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "      ") && strings.HasSuffix(trimmed, ":") {
			section = strings.TrimSuffix(trimmed, ":")
			inPackages = false
			continue
		}
		if strings.HasPrefix(trimmed, "status:") {
			status = strings.TrimSpace(strings.TrimPrefix(trimmed, "status:"))
			continue
		}
		if strings.HasPrefix(trimmed, "url:") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "url:"))
			switch section {
			case "source":
				sourceURL = value
			case "release":
				releaseURL = value
			default:
				sourceURL = value
			}
			continue
		}
		if trimmed == "packages:" {
			inPackages = true
			continue
		}
		if !inPackages || !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		pkg := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		if pkg == "" || !queryMatchesText(query, pkg, repo, status) {
			continue
		}
		resultURL := "https://index.ros.org/p/" + url.PathEscape(pkg) + "/"
		hits = append(hits, Hit{
			ID:      pkg,
			Source:  "ROS Packages",
			Title:   pkg,
			Summary: strings.TrimSpace(strings.Join(nonEmptyStrings([]string{"repository " + repo, status}), "; ")),
			URL:     resultURL,
			Score:   1 / float64(len(hits)+1),
			Metadata: map[string]string{
				"database":    "ros_packages",
				"repository":  repo,
				"status":      status,
				"source_url":  sourceURL,
				"release_url": releaseURL,
			},
			Provenance: Provenance{Source: "ROS Index", URL: resultURL},
		})
	}
	return hits
}

func queryMatchesText(query string, values ...string) bool {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return true
	}
	haystack := strings.ToLower(strings.Join(values, " "))
	if strings.Contains(haystack, query) {
		return true
	}
	for _, token := range strings.Fields(query) {
		token = strings.Trim(token, " ,;:()[]{}")
		if token != "" && !strings.Contains(haystack, token) {
			return false
		}
	}
	return true
}

func openReviewContentField(content map[string]any, key string) string {
	value := content[key]
	if text := stringFromAny(value); text != "" && !strings.HasPrefix(text, "map[") {
		return text
	}
	if record := mapFromAny(value); len(record) > 0 {
		return firstField(record, "value", "values", "text")
	}
	return ""
}

func ensemblSpecies(organism string) string {
	organism = strings.TrimSpace(strings.ToLower(organism))
	switch organism {
	case "", "human", "homo sapiens", "9606":
		return "homo_sapiens"
	case "mouse", "mus musculus", "10090":
		return "mus_musculus"
	default:
		return strings.ReplaceAll(organism, " ", "_")
	}
}

func ensemblURLSpecies(species string) string {
	parts := strings.Split(species, "_")
	for idx, part := range parts {
		if part == "" {
			continue
		}
		parts[idx] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, "_")
}

func stringTaxonID(organism string) string {
	organism = strings.TrimSpace(strings.ToLower(organism))
	switch organism {
	case "", "human", "homo sapiens", "9606":
		return "9606"
	case "mouse", "mus musculus", "10090":
		return "10090"
	default:
		return organism
	}
}

func ucscGenome(organism string) string {
	organism = strings.TrimSpace(strings.ToLower(organism))
	switch organism {
	case "", "human", "homo sapiens", "grch38", "hg38", "9606":
		return "hg38"
	case "grch37", "hg19":
		return "hg19"
	case "mouse", "mus musculus", "mm39", "10090":
		return "mm39"
	case "mm10":
		return "mm10"
	default:
		return organism
	}
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, part := range typed {
			if value := stringFromAny(part); value != "" {
				parts = append(parts, value)
			}
		}
		return strings.Join(parts, ", ")
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func nonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !strings.HasSuffix(value, " ") {
			result = append(result, value)
		}
	}
	return result
}

func firstField(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringFromAny(item[key]); value != "" {
			return value
		}
	}
	return ""
}

func firstRecordField(value any, keys ...string) string {
	records := recordsFromAny(value)
	if len(records) == 0 {
		return stringFromAny(value)
	}
	return firstField(records[0], keys...)
}

func recordsFromAny(value any, keys ...string) []map[string]any {
	switch typed := value.(type) {
	case []any:
		records := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if record := mapFromAny(item); len(record) > 0 {
				records = append(records, record)
			}
		}
		return records
	case []map[string]any:
		return typed
	case map[string]any:
		for _, key := range keys {
			if records := recordsFromAny(typed[key], keys...); len(records) > 0 {
				return records
			}
		}
		return nil
	default:
		return nil
	}
}

func cleanText(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	replacer := strings.NewReplacer(
		"<p>", " ",
		"</p>", " ",
		"<br>", " ",
		"<br/>", " ",
		"<br />", " ",
		"&nbsp;", " ",
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
	)
	value = replacer.Replace(value)
	for {
		start := strings.Index(value, "<")
		end := strings.Index(value, ">")
		if start < 0 || end < 0 || end < start {
			break
		}
		value = value[:start] + " " + value[end+1:]
	}
	return strings.Join(strings.Fields(value), " ")
}

func materialElements(query string) []string {
	fields := strings.FieldsFunc(query, func(r rune) bool {
		return r == ',' || r == ';' || r == '+' || r == ' ' || r == '\t' || r == '\n'
	})
	elements := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, "[](){}")
		if field == "" {
			continue
		}
		if len(field) == 1 {
			elements = append(elements, strings.ToUpper(field))
			continue
		}
		elements = append(elements, strings.ToUpper(field[:1])+strings.ToLower(field[1:]))
	}
	return elements
}

func recordsFromObjectValues(value any) []map[string]any {
	root := mapFromAny(value)
	if len(root) == 0 {
		return nil
	}
	records := make([]map[string]any, 0, len(root))
	for _, item := range root {
		if record := mapFromAny(item); len(record) > 0 {
			records = append(records, record)
		}
	}
	return records
}

func mapFromAny(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return nil
}

func nestedField(item map[string]any, path string) string {
	var current any = item
	for _, part := range strings.Split(path, ".") {
		switch typed := current.(type) {
		case map[string]any:
			current = typed[part]
		case []any:
			if len(typed) == 0 {
				return ""
			}
			current = typed[0]
			if part != "" {
				if nested := nestedField(map[string]any{"value": current}, "value."+part); nested != "" {
					return nested
				}
				return ""
			}
		default:
			return ""
		}
	}
	return stringFromAny(current)
}

func floatFromAny(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		number, _ := typed.Float64()
		return number
	default:
		return 0
	}
}

func siftsBestStructureHits(payload any, accession string, limit int) []Hit {
	root := mapFromAny(payload)
	records := recordsFromAny(root[accession])
	if len(records) == 0 {
		records = recordsFromAny(root[strings.ToUpper(accession)])
	}
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, item := range records {
		pdbID := strings.ToUpper(firstField(item, "pdb_id", "pdbId", "pdb"))
		if pdbID == "" {
			continue
		}
		chain := firstField(item, "chain_id", "chainId", "chain")
		unpStart := firstField(item, "unp_start", "unpStart")
		unpEnd := firstField(item, "unp_end", "unpEnd")
		start := firstField(item, "start")
		end := firstField(item, "end")
		method := firstField(item, "experimental_method", "experimentalMethod")
		resolution := firstField(item, "resolution")
		resultURL := "https://www.ebi.ac.uk/pdbe/entry/pdb/" + strings.ToLower(url.PathEscape(pdbID))
		hits = append(hits, Hit{
			ID:     accession + ":" + pdbID,
			Source: "SIFTS",
			Title:  accession + " -> " + pdbID,
			Summary: strings.TrimSpace(strings.Join(nonEmptyStrings([]string{
				"chain " + chain,
				"UniProt " + unpStart + "-" + unpEnd,
				"PDB " + start + "-" + end,
				method,
				"resolution " + resolution,
			}), "; ")),
			URL:   resultURL,
			Score: 1 / float64(idx+1),
			Metadata: map[string]string{
				"database":   "sifts",
				"uniprot":    accession,
				"pdb":        pdbID,
				"chain":      chain,
				"unp_start":  unpStart,
				"unp_end":    unpEnd,
				"resolution": resolution,
			},
			Provenance: Provenance{Source: "SIFTS", URL: resultURL},
		})
	}
	return hits
}

func siftsMappingHits(payload any, accession string, limit int) []Hit {
	root := mapFromAny(payload)
	hits := make([]Hit, 0, limit)
	for topID, value := range root {
		if len(hits) >= limit {
			break
		}
		container := mapFromAny(value)
		if len(container) == 0 {
			continue
		}
		pdbMap := mapFromAny(container["PDB"])
		if len(pdbMap) > 0 {
			for pdbID, mappingsValue := range pdbMap {
				if len(hits) >= limit {
					break
				}
				mappings := recordsFromAny(mappingsValue)
				mapping := map[string]any{}
				if len(mappings) > 0 {
					mapping = mappings[0]
				}
				hits = append(hits, siftsHit(topID, pdbID, mapping, len(hits)))
			}
			continue
		}
		uniprotMap := mapFromAny(container["UniProt"])
		for uniprotID, recordValue := range uniprotMap {
			if len(hits) >= limit {
				break
			}
			record := mapFromAny(recordValue)
			mappings := recordsFromAny(record["mappings"])
			mapping := map[string]any{}
			if len(mappings) > 0 {
				mapping = mappings[0]
			}
			hits = append(hits, siftsHit(uniprotID, topID, mapping, len(hits)))
		}
	}
	if len(hits) == 0 {
		_ = accession
	}
	return hits
}

func siftsHit(uniprotID string, pdbID string, mapping map[string]any, idx int) Hit {
	pdbID = strings.ToUpper(pdbID)
	chain := firstField(mapping, "chain_id", "chainId", "chain")
	unpStart := firstField(mapping, "unp_start", "unpStart")
	unpEnd := firstField(mapping, "unp_end", "unpEnd")
	start := firstField(mapping, "start")
	end := firstField(mapping, "end")
	if nestedStart := nestedField(mapping, "start.residue_number"); nestedStart != "" {
		start = nestedStart
	}
	if nestedEnd := nestedField(mapping, "end.residue_number"); nestedEnd != "" {
		end = nestedEnd
	}
	resultURL := "https://www.ebi.ac.uk/pdbe/entry/pdb/" + strings.ToLower(url.PathEscape(pdbID))
	return Hit{
		ID:     uniprotID + ":" + pdbID,
		Source: "SIFTS",
		Title:  uniprotID + " -> " + pdbID,
		Summary: strings.TrimSpace(strings.Join(nonEmptyStrings([]string{
			"chain " + chain,
			"UniProt " + unpStart + "-" + unpEnd,
			"PDB " + start + "-" + end,
		}), "; ")),
		URL:   resultURL,
		Score: 1 / float64(idx+1),
		Metadata: map[string]string{
			"database":  "sifts",
			"uniprot":   uniprotID,
			"pdb":       pdbID,
			"chain":     chain,
			"unp_start": unpStart,
			"unp_end":   unpEnd,
		},
		Provenance: Provenance{Source: "SIFTS", URL: resultURL},
	}
}

func dbSNPMafSummary(value any) string {
	records := recordsFromAny(value)
	if len(records) == 0 {
		return stringFromAny(value)
	}
	record := records[0]
	study := firstField(record, "study", "source")
	frequency := firstField(record, "freq", "frequency", "maf")
	return strings.TrimSpace(strings.Join(nonEmptyStrings([]string{study, frequency}), " "))
}

func sureChEMBLDocumentHits(payload any, limit int) []Hit {
	root := mapFromAny(payload)
	results := mapFromAny(root["results"])
	records := recordsFromAny(results["documents"], "documents")
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, document := range records {
		id := firstField(document, "docId", "doc_id", "id", "patentId")
		metadata := mapFromAny(document["metadata"])
		title := firstTextFromBlocks(metadata["titles"])
		if title == "" {
			title = firstField(document, "title", "name")
		}
		if title == "" {
			title = "SureChEMBL patent " + id
		}
		summary := firstTextFromBlocks(metadata["abstracts"])
		if summary == "" {
			summary = firstField(document, "abstract", "summary")
		}
		resultURL := "https://www.surechembl.org/patent/" + url.PathEscape(id)
		if id == "" {
			resultURL = "https://www.surechembl.org"
		}
		hits = append(hits, Hit{
			ID:      id,
			Source:  "SureChEMBL",
			Title:   title,
			Summary: summary,
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database": "surechembl",
				"type":     "patent",
			},
			Provenance: Provenance{Source: "SureChEMBL", URL: resultURL},
		})
	}
	return hits
}

func sureChEMBLStructureHits(payload any, limit int) []Hit {
	root := mapFromAny(payload)
	results := mapFromAny(root["results"])
	records := recordsFromAny(results["structures"], "structures", "compounds")
	if len(records) > limit {
		records = records[:limit]
	}
	hits := make([]Hit, 0, len(records))
	for idx, structure := range records {
		id := firstField(structure, "chemblId", "chembl_id", "schemblId", "id")
		title := firstNonEmpty([]string{
			firstField(structure, "name", "prefName", "preferredName"),
			id,
		})
		resultURL := "https://www.surechembl.org/chemical/" + url.PathEscape(id)
		if id == "" {
			resultURL = "https://www.surechembl.org"
		}
		hits = append(hits, Hit{
			ID:      id,
			Source:  "SureChEMBL",
			Title:   title,
			Summary: firstField(structure, "smiles", "canonicalSmiles", "inchi"),
			URL:     resultURL,
			Score:   1 / float64(idx+1),
			Metadata: map[string]string{
				"database": "surechembl",
				"type":     "structure",
			},
			Provenance: Provenance{Source: "SureChEMBL", URL: resultURL},
		})
	}
	return hits
}

func firstTextFromBlocks(value any) string {
	blocks, ok := value.([]any)
	if !ok {
		return stringFromAny(value)
	}
	for _, blockValue := range blocks {
		block := mapFromAny(blockValue)
		for _, key := range []string{"titles", "abstracts", "values", "text"} {
			if value := stringFromAny(block[key]); value != "" {
				return value
			}
		}
	}
	return ""
}
