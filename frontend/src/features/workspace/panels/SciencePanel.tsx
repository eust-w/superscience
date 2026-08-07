import katex from "katex";
import "katex/dist/katex.min.css";
import { AlertTriangle, Database, ExternalLink, FlaskConical, Save, Search, Send } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import type { ScienceDatabase, ScienceHit, Session } from "../../../lib/api";
import { client } from "../../../lib/client";

type RendererKind = "chemistry" | "structure" | "genome" | "equation" | "record";

const chemistrySources = new Set(["pubchem", "chembl", "chebi", "bindingdb", "surechembl"]);
const structureSources = new Set(["pdb", "pdbe", "alphafold", "sifts"]);
const genomeSources = new Set(["ensembl", "clinvar", "dbsnp", "gnomad", "ucsc", "mygene", "myvariant"]);

export function SciencePanel(props: {
  databases: ScienceDatabase[];
  session: Session | null;
  onSend: (content: string) => Promise<void>;
  onArtifactCreated?: () => Promise<void>;
}) {
  const [database, setDatabase] = useState("");
  const [query, setQuery] = useState("");
  const [hits, setHits] = useState<ScienceHit[]>([]);
  const [selected, setSelected] = useState<ScienceHit | null>(null);
  const [status, setStatus] = useState<"idle" | "searching" | "approval" | "ready" | "error">("idle");
  const [error, setError] = useState("");
  const [artifactStatus, setArtifactStatus] = useState("");

  useEffect(() => {
    if (!database && props.databases[0]) setDatabase(props.databases[0].id);
  }, [database, props.databases]);

  async function search() {
    if (!database || !query.trim()) return;
    setStatus("searching");
    setError("");
	setArtifactStatus("");
    try {
		const result = await client.searchScience(database, query.trim(), 20, props.session?.id);
		if ("permissionId" in result) {
			setHits([]);
			setSelected(null);
			setStatus("approval");
			setArtifactStatus(`Network approval required: ${result.permissionId}`);
			await props.onArtifactCreated?.();
			return;
		}
      setHits(result);
      setSelected(result[0] ?? null);
      setStatus("ready");
    } catch (searchError) {
      setHits([]);
      setSelected(null);
      setStatus("error");
      setError(searchError instanceof Error ? searchError.message : "The connector search failed.");
    }
  }

  async function saveArtifact() {
    if (!selected) return;
    setArtifactStatus("Saving...");
    try {
      const artifact = await client.createArtifact({
        sessionId: props.session?.id,
        type: rendererKind(selected),
        title: selected.title,
        content: JSON.stringify(selected),
        metadata: JSON.stringify({ provenance: selected.provenance, source: selected.source })
      });
      setArtifactStatus(`Saved ${artifact.id}`);
      await props.onArtifactCreated?.();
    } catch (saveError) {
      setArtifactStatus(saveError instanceof Error ? saveError.message : "Artifact save failed.");
    }
  }

  return (
    <div className="pane-body science-panel">
      <div className="query-row">
        <select value={database} onChange={(event) => setDatabase(event.target.value)} aria-label="Science database">
          {props.databases.map((db) => <option key={db.id} value={db.id}>{db.name}</option>)}
        </select>
        <input value={query} onChange={(event) => setQuery(event.target.value)} onKeyDown={(event) => event.key === "Enter" && void search()} placeholder="Search terms" />
        <button className="primary" onClick={() => void search()} disabled={!database || !query.trim() || status === "searching"}>
          <Search size={13} /> {status === "searching" ? "Searching" : "Search"}
        </button>
      </div>
      {error ? <div className="panel-error" role="alert"><AlertTriangle size={14} /> {error}</div> : null}
	  {status === "approval" ? <div className="panel-status" role="status">Approve the pending network request, then run the same search again.</div> : null}
      <div className="science-results" aria-busy={status === "searching"}>
        {hits.map((hit) => (
          <button key={`${hit.source}:${hit.id}`} className={selected?.id === hit.id && selected.source === hit.source ? "active" : ""} onClick={() => setSelected(hit)}>
            <strong>{hit.title}</strong>
            <span>{hit.source}{Number.isFinite(hit.score) ? ` · score ${hit.score.toFixed(2)}` : ""}</span>
          </button>
        ))}
        {status === "ready" && hits.length === 0 ? <div className="empty">The live connector returned no records.</div> : null}
      </div>
      <div className="artifact-viewer">
        <ScientificRenderer hit={selected} />
        <div className="panel-action-row">
          <span className="muted">{artifactStatus}</span>
          <button className="secondary" disabled={!selected} onClick={() => void saveArtifact()}><Save size={13} /> Save artifact</button>
          <button className="secondary" disabled={!selected} onClick={() => selected && void props.onSend(`Use this result with provenance: ${selected.title}\n${selected.url}`)}>
            <Send size={13} /> Send
          </button>
        </div>
      </div>
    </div>
  );
}

export function ScientificRenderer({ hit }: { hit: ScienceHit | null }) {
  const kind = useMemo(() => hit ? rendererKind(hit) : "record", [hit]);
  if (!hit) return <div className="renderer-empty"><Database size={18} /><span>Select a live search result.</span></div>;
  return (
    <div className="renderer-record">
      <div className="artifact-tabs"><span>{kind}</span><span>{hit.source}</span><span>{hit.provenance.source}</span></div>
      <h3>{hit.title}</h3>
      {kind === "chemistry" ? <ChemistryRenderer hit={hit} /> : null}
      {kind === "structure" ? <StructureRenderer hit={hit} /> : null}
      {kind === "equation" ? <EquationRenderer hit={hit} /> : null}
      {kind === "genome" ? <GenomeRenderer hit={hit} /> : null}
      <p>{hit.summary || "The connector returned no summary."}</p>
      <div className="renderer-grid">
        {Object.entries(hit.metadata).map(([key, value]) => <span key={key}><strong>{key}</strong>{value || "n/a"}</span>)}
      </div>
      <a href={hit.provenance.url || hit.url} target="_blank" rel="noreferrer"><ExternalLink size={12} /> Open source</a>
    </div>
  );
}

function rendererKind(hit: ScienceHit): RendererKind {
  const source = (hit.metadata.database || hit.source).toLowerCase().replaceAll("_", "");
  if ([...chemistrySources].some((item) => source.includes(item))) return "chemistry";
  if ([...structureSources].some((item) => source.includes(item))) return "structure";
  if ([...genomeSources].some((item) => source.includes(item))) return "genome";
  if (hit.metadata.latex || hit.metadata.equation) return "equation";
  return "record";
}

function ChemistryRenderer({ hit }: { hit: ScienceHit }) {
  const cid = firstMetadata(hit, ["cid", "pubchem_cid", "compound_id"]) || (/^\d+$/.test(hit.id) ? hit.id : "");
  if (!cid) return <div className="renderer-unavailable"><FlaskConical size={16} /> No compound identifier was returned for 2D rendering.</div>;
  return <img className="molecule-image" src={`https://pubchem.ncbi.nlm.nih.gov/rest/pug/compound/cid/${encodeURIComponent(cid)}/PNG?image_size=large`} alt={`2D structure for PubChem CID ${cid}`} />;
}

function StructureRenderer({ hit }: { hit: ScienceHit }) {
  const pdb = firstMetadata(hit, ["pdb", "pdb_id", "accession"]) || hit.id;
  if (!/^[a-z0-9]{4,12}$/i.test(pdb)) return <div className="renderer-unavailable">No supported structure accession was returned.</div>;
  return <iframe className="structure-frame" title={`3D structure ${pdb}`} src={`https://molstar.org/viewer/?pdb=${encodeURIComponent(pdb)}`} loading="lazy" />;
}

function GenomeRenderer({ hit }: { hit: ScienceHit }) {
  const locus = firstMetadata(hit, ["locus", "location", "chromosome", "gene"]);
  return <div className="genome-record"><strong>{locus || hit.id}</strong><span>{hit.metadata.organism || hit.metadata.species || "Genome record"}</span></div>;
}

function EquationRenderer({ hit }: { hit: ScienceHit }) {
  const expression = hit.metadata.latex || hit.metadata.equation || "";
  let html = "";
  try {
    html = katex.renderToString(expression, { throwOnError: true, displayMode: true, output: "html" });
  } catch {
    return <div className="renderer-unavailable">The returned LaTeX expression is invalid.</div>;
  }
  return <div className="equation-renderer" dangerouslySetInnerHTML={{ __html: html }} />;
}

function firstMetadata(hit: ScienceHit, keys: string[]) {
  for (const key of keys) {
    const value = hit.metadata[key];
    if (value) return value;
  }
  return "";
}
