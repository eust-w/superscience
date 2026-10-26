import { useEffect, useMemo, useState } from "react";
import {
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  CloudCog,
  KeyRound,
  Languages,
  LoaderCircle,
  ShieldCheck
} from "lucide-react";
import type { ModelProfile, ProviderProfile } from "../../lib/api";
import { loadLocale, resources, saveLocale, type Locale } from "../../i18n/resources";

export type ProviderVerificationResult = {
  ok: boolean;
  status: string;
  message: string;
  model?: string;
  latencyMs?: number;
};

export type ProviderSetupInput = {
  provider: string;
  endpoint: string;
  model: string;
  key: string;
};

type Props = {
  providers: ProviderProfile[];
  initialError?: string;
  loadModels: (providerId: string) => Promise<ModelProfile[]>;
  onVerify: (input: ProviderSetupInput) => Promise<ProviderVerificationResult>;
  onOpenLocalTools?: () => void;
};

export function OnboardingScreen(_props: Props) {
	const [locale, setLocale] = useState<Locale>(loadLocale);
	const copy = resources[locale].onboarding;
	function chooseLocale(next: Locale) {
		setLocale(next);
		saveLocale(next);
	}
  const providers = useMemo(
    () => _props.providers.filter((provider) => provider.enabled && !["mock", "local", "ollama"].includes(provider.id)),
    [_props.providers]
  );
  const preferredProviderID = providers.find((provider) => provider.id === "openai")?.id ?? providers[0]?.id ?? "";
  const [providerID, setProviderID] = useState(preferredProviderID);
  const selectedProvider = providers.find((provider) => provider.id === providerID) ?? providers[0];
  const [endpoint, setEndpoint] = useState(selectedProvider?.baseURL ?? "");
  const [model, setModel] = useState("");
  const [key, setKey] = useState("");
  const [models, setModels] = useState<ModelProfile[]>([]);
  const [loadingModels, setLoadingModels] = useState(false);
  const [verifying, setVerifying] = useState(false);
  const [result, setResult] = useState<ProviderVerificationResult | null>(null);

  useEffect(() => {
    if (!providerID && preferredProviderID) setProviderID(preferredProviderID);
  }, [providerID, preferredProviderID]);

  useEffect(() => {
    const provider = providers.find((item) => item.id === providerID);
    setEndpoint(provider?.baseURL ?? "");
    setModel("");
    setModels([]);
    setResult(null);
    if (!providerID) return;
    let active = true;
    setLoadingModels(true);
    void _props
      .loadModels(providerID)
      .then((rows) => {
        if (!active) return;
		const catalog = Array.isArray(rows) ? rows : [];
		setModels(catalog);
		setModel("");
      })
      .catch((error: unknown) => {
        if (!active) return;
        setResult({ ok: false, status: "models_error", message: error instanceof Error ? error.message : "Model catalog unavailable" });
      })
      .finally(() => {
        if (active) setLoadingModels(false);
      });
    return () => {
      active = false;
    };
  }, [providerID]);

  async function verify() {
    if (!providerID || !model.trim() || !key.trim()) {
      setResult({ ok: false, status: "incomplete", message: "Provider, model, and API key are required." });
      return;
    }
    setVerifying(true);
    setResult(null);
    try {
      setResult(await _props.onVerify({ provider: providerID, endpoint: endpoint.trim(), model: model.trim(), key: key.trim() }));
    } catch (error) {
      setResult({ ok: false, status: "error", message: error instanceof Error ? error.message : "Provider verification failed" });
    } finally {
      setVerifying(false);
    }
  }

  return (
    <main className="onboarding-shell">
      <header className="onboarding-titlebar">
        <div className="brand-lockup"><span className="brand-mark"><CloudCog size={17} /></span><strong>Superscience</strong></div>
		<div className="language-control" role="group" aria-label="Language"><Languages size={14} /><button className={locale === "en" ? "active" : ""} onClick={() => chooseLocale("en")}>English</button><button className={locale === "zh-CN" ? "active" : ""} onClick={() => chooseLocale("zh-CN")}>中文</button></div>
      </header>
      <div className="onboarding-layout">
        <aside className="onboarding-steps" aria-label="Setup progress">
          <div className="onboarding-intro">
			<span className="eyebrow">{copy.eyebrow}</span>
			<h1>{copy.title}</h1>
			<p>{copy.intro}</p>
          </div>
          <ol>
			<li className="active"><span>1</span><div><strong>{copy.providerStep}</strong><small>{copy.providerStepDetail}</small></div></li>
			<li className={result?.ok ? "complete" : ""}><span>2</span><div><strong>{copy.verifyStep}</strong><small>{copy.verifyStepDetail}</small></div></li>
			<li><span>3</span><div><strong>{copy.systemStep}</strong><small>{copy.systemStepDetail}</small></div></li>
			<li><span>4</span><div><strong>{copy.workspaceStep}</strong><small>{copy.workspaceStepDetail}</small></div></li>
          </ol>
		  <div className="security-note"><ShieldCheck size={16} /><span>{copy.security}</span></div>
        </aside>
        <section className="onboarding-form-panel">
          <div className="section-heading">
			<div><span className="eyebrow">{copy.requiredSetup}</span><h2>{copy.connection}</h2></div>
			<span className="required-badge"><KeyRound size={13} /> {copy.required}</span>
          </div>
          {_props.initialError && !result && <div className="verification-banner warn"><AlertTriangle size={15} /><span>{_props.initialError}</span></div>}
          <label>
			<span>{copy.provider}</span>
            <select value={providerID} onChange={(event) => setProviderID(event.target.value)}>
              {providers.map((provider) => <option key={provider.id} value={provider.id}>{provider.displayName}</option>)}
            </select>
          </label>
          <label>
			<span>{copy.endpoint}</span>
			<input value={endpoint} onChange={(event) => setEndpoint(event.target.value)} placeholder={copy.endpointPlaceholder} />
          </label>
          <label>
			<span>{copy.model}</span>
            <div className="field-with-state">
			  <input list="provider-models" value={model} onChange={(event) => setModel(event.target.value)} placeholder={copy.modelPlaceholder} />
              {loadingModels && <LoaderCircle className="spin" size={15} />}
            </div>
            <datalist id="provider-models">{models.map((item) => <option key={item.id} value={item.id}>{item.displayName}</option>)}</datalist>
          </label>
          <label>
			<span>{copy.apiKey}</span>
			<input type="password" autoComplete="off" value={key} onChange={(event) => setKey(event.target.value)} placeholder={copy.keyPlaceholder} />
          </label>
          {result && (
            <div className={`verification-banner ${result.ok ? "ok" : "warn"}`} role="status">
              {result.ok ? <CheckCircle2 size={15} /> : <AlertTriangle size={15} />}
			  <div><strong>{result.ok ? copy.verified : copy.failed}</strong><span>{result.message}</span></div>
              {result.latencyMs !== undefined && <small>{result.latencyMs} ms</small>}
            </div>
          )}
          <div className="onboarding-actions">
			<p>{copy.noGeneration}</p>
            <button className="primary" type="button" disabled={verifying || providers.length === 0} onClick={() => void verify()}>
              {verifying ? <LoaderCircle className="spin" size={15} /> : <ShieldCheck size={15} />}
			  {copy.continue}
              <ArrowRight size={15} />
            </button>
          </div>
          <div className="onboarding-actions">
            <p>{copy.localToolsDetail}</p>
            <button className="secondary" type="button" disabled={!_props.onOpenLocalTools || verifying} onClick={_props.onOpenLocalTools}>{copy.localTools}</button>
          </div>
        </section>
      </div>
    </main>
  );
}
