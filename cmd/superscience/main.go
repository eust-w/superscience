package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	cloudpkg "superscience/cloud"
	cloudobjects "superscience/cloud/objectstore"
	cloudpostgres "superscience/cloud/postgres"
	acpserver "superscience/internal/acp"
	agentpkg "superscience/internal/agent"
	"superscience/internal/buildinfo"
	"superscience/internal/contracts"
	mcpclient "superscience/internal/mcp"
	"superscience/internal/providers"
	releasepkg "superscience/internal/release"
	appruntime "superscience/internal/runtime"
	"superscience/internal/secrets"
	sharepkg "superscience/internal/share"
	"superscience/internal/skills"
	"superscience/internal/store"
	"superscience/internal/tools"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	return runCommand(context.Background(), os.Args[1:], os.Stdout, os.Stderr)
}

func runCommand(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	command := "web"
	if len(args) > 0 {
		command = args[0]
	}
	switch command {
	case "web", "serve":
		return runLocalServer(ctx, command, args[1:], stdout, stderr)
	case "desktop":
		return runDesktop(ctx, args[1:], stdout, stderr)
	case "cloud":
		return runCloudServer(ctx, args[1:], stdout, stderr)
	case "version":
		fmt.Fprintf(stdout, "superscience %s\ncommit %s\nbuilt %s\n", buildinfo.DisplayVersion(), buildinfo.Commit, buildinfo.Date)
	case "generate":
		return handleGenerate(args[1:], stdout)
	case "models":
		return handleModels(ctx, args[1:], stdout)
	case "agent":
		return handleAgent(ctx, args[1:], stdout, stderr)
	case "skill":
		return handleSkill(args[1:], stdout)
	case "mcp":
		return handleMCP(ctx, args[1:], stdout, stderr)
	case "keys":
		return handleKeys(ctx, args[1:], stdout)
	case "session":
		return handleSession(ctx, args[1:], stdout)
	case "project":
		return handleProject(ctx, args[1:], stdout)
	case "run":
		return handleRun(ctx, args[1:], stdout, stderr)
	case "export":
		return handleExport(ctx, args[1:], stdout)
	case "import":
		return handleImport(ctx, args[1:], stdout)
	case "acp":
		return handleACP(ctx, args[1:], cliInput, stdout, stderr)
	case "upgrade":
		return handleUpgrade(ctx, args[1:], stdout, stderr)
	case "uninstall":
		return handleUninstall(args[1:], stdout, stderr)
	case "stats":
		providerCount, modelCount := providers.ModelStats()
		fmt.Fprintf(stdout, "providers=%d\nmodels=%d\nagents=%d\ntools=%d\n", providerCount, modelCount, len(tools.BuiltInAgents()), len(tools.BuiltIns()))
	case "debug":
		if len(args) > 1 && args[1] == "paths" {
			return handleDebugPaths(stdout)
		}
		return fmt.Errorf("unknown debug command %q", strings.Join(args[1:], " "))
	case "doctor":
		return handleDoctor(stdout)
	case "completion":
		shell := "zsh"
		if len(args) > 1 {
			shell = args[1]
		}
		return handleCompletion(shell, stdout)
	default:
		return fmt.Errorf("unknown command %q (expected web, serve, desktop, cloud, version, generate, models, agent, skill, mcp, keys, session, project, run, export, import, acp, upgrade, uninstall, stats, debug, doctor, completion)", command)
	}
	return nil
}

func runLocalServer(ctx context.Context, command string, args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet(command, flag.ExitOnError)
	fs.SetOutput(stderr)
	port := fs.Int("port", 4096, "localhost port")
	project := fs.String("project", ".", "project directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := filepath.Abs(*project)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	instance, err := appruntime.Start(ctx, appruntime.Options{Port: *port, ProjectRoot: root})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Superscience server: %s\n", instance.URL)
	if command == "web" {
		_ = appruntime.OpenBrowser(instance.URL)
	}
	<-ctx.Done()
	return nil
}

func runDesktop(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("desktop", flag.ContinueOnError)
	fs.SetOutput(stderr)
	appPath := fs.String("app", "", "path to the packaged Superscience desktop application")
	project := fs.String("project", ".", "project directory")
	wait := fs.Bool("wait", false, "wait for the desktop process to exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := filepath.Abs(*project)
	if err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("desktop project is not a directory: %s", root)
	}
	resolved, err := resolveDesktopApp(*appPath)
	if err != nil {
		return err
	}
	var command *exec.Cmd
	if runtime.GOOS == "darwin" && strings.HasSuffix(strings.ToLower(resolved), ".app") {
		command = exec.CommandContext(ctx, "open", "-n", resolved, "--args", root)
	} else {
		command = exec.CommandContext(ctx, resolved, root)
	}
	command.Stdout = stdout
	command.Stderr = stderr
	if *wait {
		return command.Run()
	}
	if err := command.Start(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Superscience desktop launched: %s\n", resolved)
	return nil
}

func resolveDesktopApp(explicit string) (string, error) {
	candidates := []string{strings.TrimSpace(explicit), strings.TrimSpace(os.Getenv("SUPERSCIENCE_DESKTOP_APP"))}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "build", "bin", "Superscience.app"),
			filepath.Join(wd, "build", "bin", "superscience.app"),
			filepath.Join(wd, "build", "bin", "Superscience"),
		)
	}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), "Superscience.app"), filepath.Join(filepath.Dir(executable), "Superscience"))
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if _, err := os.Stat(absolute); err == nil {
			return absolute, nil
		}
	}
	return "", errors.New("Superscience desktop application was not found; build it with `wails build` or pass --app")
}

func runCloudServer(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("cloud", flag.ExitOnError)
	fs.SetOutput(stderr)
	host := fs.String("host", "127.0.0.1", "cloud bind host")
	port := fs.Int("port", 4097, "localhost cloud port")
	developmentSQLite := fs.Bool("dev-sqlite", false, "use local SQLite metadata storage for development only")
	if err := fs.Parse(args); err != nil {
		return err
	}
	localAuthSecret := strings.TrimSpace(os.Getenv("SUPERSCIENCE_CLOUD_LOCAL_AUTH_SECRET"))
	if localAuthSecret != "" && len(localAuthSecret) < 24 {
		return errors.New("SUPERSCIENCE_CLOUD_LOCAL_AUTH_SECRET must be configured with at least 24 characters")
	}
	oidcAuthenticator, err := cloudOIDCFromEnv(ctx)
	if err != nil {
		return err
	}
	if localAuthSecret == "" && oidcAuthenticator == nil {
		return errors.New("cloud authentication is not configured; set OIDC issuer/client ID or a local bootstrap secret")
	}
	metadata, closeMetadata, err := openCloudMetadataStore(ctx, *developmentSQLite)
	if err != nil {
		return err
	}
	defer closeMetadata()
	objects, err := openCloudObjectStore(ctx, *developmentSQLite)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", *host, *port))
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	allowedHosts := []string{*host}
	for _, configured := range strings.Split(os.Getenv("SUPERSCIENCE_CLOUD_ALLOWED_HOSTS"), ",") {
		if configured = strings.TrimSpace(configured); configured != "" {
			allowedHosts = append(allowedHosts, configured)
		}
	}
	server := &http.Server{Handler: cloudpkg.New(cloudpkg.Options{
		Store: metadata, Objects: objects, LocalAuthSecret: localAuthSecret, OIDC: oidcAuthenticator, AllowedHosts: allowedHosts,
	}).Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(stderr, "superscience cloud error: %v\n", err)
		}
	}()
	fmt.Fprintf(stdout, "Superscience cloud: http://%s\n", ln.Addr().String())
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

func openCloudObjectStore(ctx context.Context, developmentSQLite bool) (cloudpkg.ObjectStore, error) {
	endpoint := strings.TrimSpace(os.Getenv("SUPERSCIENCE_CLOUD_S3_ENDPOINT"))
	bucket := strings.TrimSpace(os.Getenv("SUPERSCIENCE_CLOUD_S3_BUCKET"))
	accessKey := strings.TrimSpace(os.Getenv("SUPERSCIENCE_CLOUD_S3_ACCESS_KEY"))
	secretKey := strings.TrimSpace(os.Getenv("SUPERSCIENCE_CLOUD_S3_SECRET_KEY"))
	if endpoint == "" && bucket == "" && accessKey == "" && secretKey == "" {
		if developmentSQLite {
			return nil, nil
		}
		return nil, errors.New("SUPERSCIENCE_CLOUD_S3_ENDPOINT is required for production cloud storage")
	}
	required := []struct {
		name  string
		value string
	}{
		{name: "SUPERSCIENCE_CLOUD_S3_ENDPOINT", value: endpoint},
		{name: "SUPERSCIENCE_CLOUD_S3_BUCKET", value: bucket},
		{name: "SUPERSCIENCE_CLOUD_S3_ACCESS_KEY", value: accessKey},
		{name: "SUPERSCIENCE_CLOUD_S3_SECRET_KEY", value: secretKey},
	}
	for _, field := range required {
		if field.value == "" {
			return nil, fmt.Errorf("%s is required when S3 object storage is configured", field.name)
		}
	}
	pathStyle, err := cloudBoolEnv("SUPERSCIENCE_CLOUD_S3_PATH_STYLE", false)
	if err != nil {
		return nil, err
	}
	ensureBucket, err := cloudBoolEnv("SUPERSCIENCE_CLOUD_S3_CREATE_BUCKET", false)
	if err != nil {
		return nil, err
	}
	return cloudobjects.NewS3(ctx, cloudobjects.S3Config{
		Endpoint: endpoint, Region: strings.TrimSpace(os.Getenv("SUPERSCIENCE_CLOUD_S3_REGION")), Bucket: bucket,
		AccessKey: accessKey, SecretKey: secretKey, SessionToken: strings.TrimSpace(os.Getenv("SUPERSCIENCE_CLOUD_S3_SESSION_TOKEN")),
		PathStyle: pathStyle, EnsureBucket: ensureBucket,
	})
}

func cloudBoolEnv(name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return parsed, nil
}

func openCloudMetadataStore(ctx context.Context, developmentSQLite bool) (cloudpkg.MetadataStore, func() error, error) {
	if developmentSQLite {
		db, err := openDefaultStore(ctx)
		if err != nil {
			return nil, nil, err
		}
		return db, db.Close, nil
	}
	databaseURL := strings.TrimSpace(os.Getenv("SUPERSCIENCE_CLOUD_DATABASE_URL"))
	if databaseURL == "" {
		return nil, nil, errors.New("SUPERSCIENCE_CLOUD_DATABASE_URL is required; use --dev-sqlite only for local development")
	}
	db, err := cloudpostgres.Open(ctx, databaseURL)
	if err != nil {
		return nil, nil, err
	}
	return db, db.Close, nil
}

func cloudOIDCFromEnv(ctx context.Context) (cloudpkg.OIDCAuthenticator, error) {
	issuer := strings.TrimSpace(os.Getenv("SUPERSCIENCE_CLOUD_OIDC_ISSUER"))
	clientID := strings.TrimSpace(os.Getenv("SUPERSCIENCE_CLOUD_OIDC_CLIENT_ID"))
	if issuer == "" && clientID == "" {
		return nil, nil
	}
	if issuer == "" {
		return nil, errors.New("SUPERSCIENCE_CLOUD_OIDC_ISSUER is required when OIDC is configured")
	}
	if clientID == "" {
		return nil, errors.New("SUPERSCIENCE_CLOUD_OIDC_CLIENT_ID is required when OIDC is configured")
	}
	domains := make([]string, 0)
	for _, domain := range strings.Split(os.Getenv("SUPERSCIENCE_CLOUD_OIDC_ALLOWED_EMAIL_DOMAINS"), ",") {
		if domain = strings.TrimSpace(domain); domain != "" {
			domains = append(domains, domain)
		}
	}
	return cloudpkg.NewOIDCAuthenticator(ctx, cloudpkg.OIDCConfig{
		Issuer: issuer, ClientID: clientID, AllowedEmailDomains: domains,
	})
}

func handleGenerate(args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "openapi" {
		return contracts.WriteOpenAPI(stdout)
	}
	return fmt.Errorf("unknown generate target %q", args[0])
}

func handleUpgrade(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repository := fs.String("repository", envOrDefault("SUPERSCIENCE_RELEASE_REPOSITORY", "eust-w/superscience"), "GitHub owner/repository")
	apiURL := fs.String("api-url", strings.TrimSpace(os.Getenv("SUPERSCIENCE_RELEASE_API_URL")), "GitHub API base URL")
	target := fs.String("target", "", "Superscience executable to replace")
	checkOnly := fs.Bool("check", false, "check for a verified release without installing it")
	confirmed := fs.Bool("yes", false, "install the verified update without another prompt")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected upgrade arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *checkOnly && *confirmed {
		return errors.New("upgrade --check cannot be combined with --yes")
	}
	if strings.TrimSpace(*target) == "" {
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		*target = executable
	}
	client := releasepkg.Client{Repository: *repository, APIBaseURL: *apiURL}
	if *checkOnly || !*confirmed {
		check, err := client.Check(ctx, buildinfo.DisplayVersion())
		if err != nil {
			return err
		}
		return writeIndented(stdout, map[string]any{
			"currentVersion":       check.CurrentVersion,
			"latestVersion":        check.LatestVersion,
			"updateAvailable":      check.UpdateAvailable,
			"asset":                check.Asset.Name,
			"target":               *target,
			"confirmationRequired": check.UpdateAvailable && !*checkOnly,
		})
	}
	result, err := client.Upgrade(ctx, buildinfo.DisplayVersion(), *target)
	if err != nil {
		return err
	}
	return writeIndented(stdout, result)
}

func handleUninstall(args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(stderr)
	target := fs.String("target", "", "Superscience executable or application bundle to remove")
	dataDir := fs.String("data-dir", "", "Superscience data directory")
	removeData := fs.Bool("remove-data", false, "also remove validated Superscience application data")
	confirmed := fs.Bool("yes", false, "perform the removal without another prompt")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected uninstall arguments: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(*target) == "" {
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		*target = executable
	}
	if strings.TrimSpace(*dataDir) == "" {
		resolved, err := appruntime.DataDir()
		if err != nil {
			return err
		}
		*dataDir = resolved
	}
	options := releasepkg.UninstallOptions{
		Target: *target, DataDir: *dataDir, RemoveData: *removeData, Confirm: *confirmed,
	}
	if !*confirmed {
		plan, err := releasepkg.PlanUninstall(options)
		if err != nil {
			return err
		}
		return writeIndented(stdout, map[string]any{
			"installationTarget":   plan.InstallationTarget,
			"dataDir":              plan.DataDir,
			"removeData":           plan.RemoveData,
			"confirmationRequired": true,
		})
	}
	result, err := releasepkg.Uninstall(options)
	if err != nil {
		return err
	}
	return writeIndented(stdout, result)
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func handleModels(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "refresh" {
		endpoint := providers.DefaultCatalogURL
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--endpoint":
				if i+1 >= len(args) {
					return errors.New("models refresh --endpoint requires a URL")
				}
				endpoint = args[i+1]
				i++
			default:
				return fmt.Errorf("unknown models refresh option %q", args[i])
			}
		}
		catalog := providers.NewCatalog(endpoint, nil)
		models, err := catalog.Refresh(ctx)
		if err != nil {
			return err
		}
		db, err := openDefaultStore(ctx)
		if err != nil {
			return err
		}
		defer db.Close()
		records := make([]store.ModelRecord, 0, len(models))
		for _, model := range models {
			records = append(records, store.ModelRecord{ID: model.ID, ProviderID: model.ProviderID, DisplayName: model.DisplayName, ContextWindow: model.ContextWindow, Capabilities: model.Capabilities, Status: model.Status})
		}
		if err := db.ReplaceModelCatalog(ctx, records); err != nil {
			return err
		}
		if err := db.SetSettings(ctx, map[string]string{"models.catalogSource": endpoint, "models.lastRefresh": fmt.Sprint(time.Now().UnixMilli())}); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "source=%s\nproviders=%d\nmodels=%d\n", endpoint, countCLIModelProviders(models), len(models))
		for _, model := range models {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%d\n", model.ProviderID, model.ID, model.Status, model.ContextWindow)
		}
		return nil
	}
	if len(args) > 0 && args[0] != "list" {
		return fmt.Errorf("unknown models command %q", args[0])
	}
	db, err := openDefaultStore(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	for _, provider := range providers.BuiltIns() {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", provider.ID, provider.DisplayName, provider.Kind)
		models := providers.Models(provider.ID)
		if persisted, err := db.ListModels(ctx, provider.ID); err == nil && len(persisted) > 0 {
			models = make([]providers.Model, 0, len(persisted))
			for _, model := range persisted {
				models = append(models, providers.Model{ID: model.ID, ProviderID: model.ProviderID, DisplayName: model.DisplayName, ContextWindow: model.ContextWindow, Capabilities: model.Capabilities, Status: model.Status})
			}
		}
		for _, model := range models {
			fmt.Fprintf(stdout, "  %s\t%s\t%d\n", model.ID, model.Status, model.ContextWindow)
		}
	}
	return nil
}

func countCLIModelProviders(models []providers.Model) int {
	seen := map[string]bool{}
	for _, model := range models {
		seen[model.ProviderID] = true
	}
	return len(seen)
}

func handleAgent(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	command := "list"
	if len(args) > 0 {
		command = args[0]
	}
	db, err := openDefaultStore(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	switch command {
	case "list":
		definitions, err := agentpkg.ListDefinitions(ctx, db)
		if err != nil {
			return err
		}
		return writeIndented(stdout, definitions)
	case "show":
		if len(args) < 2 {
			return errors.New("agent show requires an agent ID")
		}
		definition, err := agentpkg.GetDefinition(ctx, db, args[1])
		if err != nil {
			return err
		}
		return writeIndented(stdout, definition)
	case "create":
		if len(args) < 2 || strings.HasPrefix(args[1], "-") {
			return errors.New("agent create requires an agent ID")
		}
		fs := flag.NewFlagSet("agent create", flag.ContinueOnError)
		fs.SetOutput(stderr)
		name := fs.String("name", "", "display name")
		domain := fs.String("domain", "general", "scientific domain")
		description := fs.String("description", "", "agent description")
		systemPrompt := fs.String("system-prompt", "", "agent system prompt")
		reviewMode := fs.String("review", "optional", "none, optional, recommended, or required")
		enabled := fs.Bool("enabled", true, "enable the agent")
		var allowedTools stringListFlag
		fs.Var(&allowedTools, "tool", "allowed tool ID; repeat for multiple tools")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return fmt.Errorf("unexpected agent create arguments: %s", strings.Join(fs.Args(), " "))
		}
		definition, err := agentpkg.CreateDefinition(ctx, db, agentpkg.Definition{
			ID: args[1], Name: *name, Domain: *domain, Description: *description, SystemPrompt: *systemPrompt,
			Tools: allowedTools, ReviewMode: *reviewMode, Enabled: *enabled,
		})
		if err != nil {
			return err
		}
		_, _ = db.CreateAuditEvent(ctx, store.AuditEvent{Action: "agent.create", EntityID: definition.ID, EntityType: "agent", Actor: "cli"})
		return writeIndented(stdout, definition)
	case "enable", "disable":
		if len(args) < 2 {
			return fmt.Errorf("agent %s requires an agent ID", command)
		}
		enabled := command == "enable"
		definition, err := agentpkg.UpdateDefinition(ctx, db, args[1], agentpkg.DefinitionPatch{Enabled: &enabled})
		if err != nil {
			return err
		}
		_, _ = db.CreateAuditEvent(ctx, store.AuditEvent{Action: "agent." + command, EntityID: definition.ID, EntityType: "agent", Actor: "cli"})
		return writeIndented(stdout, definition)
	case "remove":
		if len(args) < 2 {
			return errors.New("agent remove requires an agent ID")
		}
		if err := agentpkg.DeleteDefinition(ctx, db, args[1]); err != nil {
			return err
		}
		_, _ = db.CreateAuditEvent(ctx, store.AuditEvent{Action: "agent.delete", EntityID: args[1], EntityType: "agent", Actor: "cli"})
		return writeIndented(stdout, map[string]any{"removed": true, "id": args[1]})
	default:
		return fmt.Errorf("unknown agent command %q", command)
	}
}

func handleSkill(args []string, stdout io.Writer) error {
	catalog, err := cliSkillCatalog()
	if err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "list" {
		return writeIndented(stdout, skills.Merge(skills.Bundled(), catalog))
	}
	switch args[0] {
	case "show":
		if len(args) < 2 {
			return errors.New("skill show requires a name")
		}
		loaded, err := skills.Load(args[1], catalog)
		if err != nil {
			return err
		}
		return writeIndented(stdout, loaded)
	case "validate":
		if len(args) < 2 {
			return errors.New("skill validate requires a SKILL.md path")
		}
		skill, err := skills.ParseFile(args[1])
		if err != nil {
			return err
		}
		return writeIndented(stdout, map[string]any{"ok": true, "skill": skill})
	case "add":
		if len(args) < 2 {
			return errors.New("skill add requires a SKILL.md path or directory")
		}
		return installCLISkill(args[1], stdout)
	case "new":
		if len(args) < 2 {
			return errors.New("skill new requires a name")
		}
		description := "User-defined Superscience skill."
		if len(args) > 2 {
			description = strings.Join(args[2:], " ")
		}
		return createCLISkill(args[1], description, stdout)
	case "edit":
		if len(args) < 2 {
			return errors.New("skill edit requires a name")
		}
		return editCLISkill(args[1])
	case "remove":
		if len(args) < 2 {
			return errors.New("skill remove requires a name")
		}
		return removeCLISkill(args[1], stdout)
	case "set-entries":
		if len(args) < 3 {
			return errors.New("skill set-entries requires a namespace and comma-separated skill names")
		}
		entries := make([]string, 0)
		for _, value := range args[2:] {
			entries = append(entries, strings.Split(value, ",")...)
		}
		manifest, err := skills.SetEntries(args[1], entries)
		if err != nil {
			return err
		}
		return writeIndented(stdout, manifest)
	default:
		return fmt.Errorf("unknown skill command %q", args[0])
	}
}

func installCLISkill(source string, stdout io.Writer) error {
	path, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		path = filepath.Join(path, "SKILL.md")
	}
	skill, err := skills.ParseFile(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) > 1_000_000 {
		return errors.New("SKILL.md exceeds the 1 MB installation limit")
	}
	root, err := cliSkillRoot()
	if err != nil {
		return err
	}
	destinationDir := filepath.Join(root, slug(skill.Name))
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		return err
	}
	destination := filepath.Join(destinationDir, "SKILL.md")
	if err := os.WriteFile(destination, data, 0o644); err != nil {
		return err
	}
	installed, err := skills.ParseFile(destination)
	if err != nil {
		return err
	}
	installed.Source = "user"
	return writeIndented(stdout, installed)
}

func createCLISkill(name, description string, stdout io.Writer) error {
	id := slug(name)
	if id == "custom" {
		return errors.New("skill name must contain letters or numbers")
	}
	root, err := cliSkillRoot()
	if err != nil {
		return err
	}
	destinationDir := filepath.Join(root, id)
	if _, err := os.Stat(destinationDir); err == nil {
		return fmt.Errorf("skill %q already exists", id)
	}
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		return err
	}
	description = strings.Join(strings.Fields(description), " ")
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n# %s\n\nApply this skill only when its scope matches the user objective. State assumptions, use available tools through their permission gates, preserve source provenance, and report unresolved evidence gaps explicitly.\n", id, description, name)
	path := filepath.Join(destinationDir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	created, err := skills.ParseFile(path)
	if err != nil {
		return err
	}
	created.Source = "user"
	return writeIndented(stdout, created)
}

func editCLISkill(name string) error {
	skill, err := findCLIUserSkill(name)
	if err != nil {
		return err
	}
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		return errors.New("EDITOR is not configured")
	}
	command := exec.Command(editor, skill.Path)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

func removeCLISkill(name string, stdout io.Writer) error {
	skill, err := findCLIUserSkill(name)
	if err != nil {
		return err
	}
	root, err := cliSkillRoot()
	if err != nil {
		return err
	}
	directory := filepath.Dir(skill.Path)
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative == "." || strings.HasPrefix(relative, "..") {
		return errors.New("refusing to remove a skill outside the user skill directory")
	}
	if err := os.RemoveAll(directory); err != nil {
		return err
	}
	return writeIndented(stdout, map[string]any{"removed": true, "name": skill.Name, "path": directory})
}

func cliSkillRoot() (string, error) {
	dataDir, err := skills.UserDataDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(dataDir, "skills")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	return root, nil
}

func findCLIUserSkill(name string) (store.SkillRecord, error) {
	loaded, err := skills.LoadUser()
	if err != nil {
		return store.SkillRecord{}, err
	}
	for _, skill := range loaded {
		if skill.Name == name || skill.ID == name {
			return skill, nil
		}
	}
	return store.SkillRecord{}, fmt.Errorf("user skill %q was not found", name)
}

func cliSkillCatalog() ([]store.SkillRecord, error) {
	userSkills, err := skills.LoadUser()
	if err != nil {
		return nil, err
	}
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	projectSkills, err := skills.LoadProject(wd)
	if err != nil {
		return nil, err
	}
	installedSkills, err := skills.LoadInstalled()
	if err != nil {
		return nil, err
	}
	catalog := append(userSkills, installedSkills...)
	return append(catalog, projectSkills...), nil
}

func handleMCP(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	command := "list"
	if len(args) > 0 {
		command = args[0]
	}
	db, err := openDefaultStore(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	switch command {
	case "list":
		servers, err := db.ListMCPServers(ctx)
		if err != nil {
			return err
		}
		return writeIndented(stdout, servers)
	case "add":
		return handleMCPAdd(ctx, db, args[1:], stdout, stderr)
	case "remove":
		if len(args) < 2 {
			return errors.New("mcp remove requires a server ID")
		}
		if err := mcpclient.CloseServer(args[1]); err != nil {
			return err
		}
		if err := db.DeleteMCPServer(ctx, args[1]); err != nil {
			return err
		}
		return writeIndented(stdout, map[string]any{"removed": true, "serverId": args[1]})
	case "discover":
		if len(args) < 2 {
			return errors.New("mcp discover requires a server ID")
		}
		server, err := db.GetMCPServer(ctx, args[1])
		if err != nil {
			return err
		}
		server, err = mcpclient.PrepareAuthenticatedServer(ctx, server, cliSecrets)
		if err != nil {
			return err
		}
		discovery, err := mcpclient.DiscoverCapabilities(ctx, server)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(discovery)
		if err != nil {
			return err
		}
		_, _ = db.UpsertMCPDiscovery(ctx, store.MCPDiscoveryCache{ServerID: server.ID, Payload: string(payload), Status: "ready"})
		return writeIndented(stdout, discovery)
	case "auth":
		if len(args) >= 3 && args[1] == "remove" {
			server, err := db.GetMCPServer(ctx, args[2])
			if err != nil {
				return err
			}
			if server.OAuthSecretRef == "" {
				return fmt.Errorf("MCP server %q has no stored OAuth credential", server.ID)
			}
			if err := cliSecrets.Delete(server.OAuthSecretRef); err != nil {
				return err
			}
			server.OAuthSecretRef = ""
			server.OAuthStatus = ""
			server, err = db.UpsertMCPServer(ctx, server)
			if err != nil {
				return err
			}
			return writeIndented(stdout, server)
		}
		return errors.New("mcp auth requires the running workbench callback server; use the MCP manager to complete OAuth PKCE")
	default:
		return fmt.Errorf("unknown mcp command %q", command)
	}
}

type stringListFlag []string

func (values *stringListFlag) String() string { return strings.Join(*values, ",") }
func (values *stringListFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func handleMCPAdd(ctx context.Context, db *store.Store, args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return errors.New("mcp add requires a server name")
	}
	name := args[0]
	fs := flag.NewFlagSet("mcp add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	transport := fs.String("transport", "stdio", "stdio, sse, or streamable-http")
	command := fs.String("command", "", "stdio executable path")
	endpoint := fs.String("url", "", "SSE or Streamable HTTP endpoint")
	var commandArgs stringListFlag
	fs.Var(&commandArgs, "arg", "stdio argument; repeat for multiple arguments")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	server := store.MCPServer{Name: name, Transport: *transport, Command: strings.TrimSpace(*command), Args: commandArgs, URL: strings.TrimSpace(*endpoint), Enabled: true}
	switch server.Transport {
	case "stdio":
		if server.Command == "" {
			return errors.New("stdio MCP server requires --command")
		}
	case "sse", "streamable-http":
		if server.URL == "" {
			return fmt.Errorf("%s MCP server requires --url", server.Transport)
		}
	default:
		return fmt.Errorf("unsupported MCP transport %q", server.Transport)
	}
	saved, err := db.UpsertMCPServer(ctx, server)
	if err != nil {
		return err
	}
	return writeIndented(stdout, saved)
}

var cliSecrets secrets.Backend = secrets.OSBackend{}
var cliInput io.Reader = os.Stdin

func handleACP(ctx context.Context, args []string, input io.Reader, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("acp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("acp does not accept positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	db, err := openDefaultStore(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	protocolCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = acpserver.Serve(protocolCtx, input, stdout, acpserver.Options{
		Store: db, Secrets: cliSecrets, Version: buildinfo.DisplayVersion(),
	})
	if errors.Is(err, context.Canceled) && protocolCtx.Err() != nil {
		return nil
	}
	return err
}

func handleKeys(ctx context.Context, args []string, stdout io.Writer) error {
	command := "list"
	if len(args) > 0 {
		command = args[0]
	}
	db, err := openDefaultStore(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	switch command {
	case "list":
		credentials, err := db.ListCredentials(ctx)
		if err != nil {
			return err
		}
		return writeIndented(stdout, credentials)
	case "add":
		if len(args) < 2 {
			return errors.New("keys add requires a provider ID")
		}
		providerID := strings.TrimSpace(args[1])
		fs := flag.NewFlagSet("keys add", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		label := fs.String("label", providerID, "credential label")
		keyEnv := fs.String("key-env", "SUPERSCIENCE_API_KEY", "environment variable containing the secret")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		secret := strings.TrimSpace(os.Getenv(*keyEnv))
		if secret == "" {
			return fmt.Errorf("credential environment variable %s is empty", *keyEnv)
		}
		if err := cliSecrets.Store(providerID, secret); err != nil {
			return err
		}
		credential, err := db.UpsertCredential(ctx, store.Credential{Provider: providerID, Label: *label, Source: "keychain", Redacted: cliRedact(secret)})
		if err != nil {
			_ = cliSecrets.Delete(providerID)
			return err
		}
		return writeIndented(stdout, credential)
	case "rm":
		if len(args) < 2 {
			return errors.New("keys rm requires a provider ID or credential ID")
		}
		credentials, err := db.ListCredentials(ctx)
		if err != nil {
			return err
		}
		target := args[1]
		for _, credential := range credentials {
			if credential.ID != target && credential.Provider != target {
				continue
			}
			if err := cliSecrets.Delete(credential.Provider); err != nil {
				return err
			}
			if err := db.DeleteCredential(ctx, credential.ID); err != nil {
				return err
			}
			return writeIndented(stdout, map[string]any{"removed": true, "provider": credential.Provider})
		}
		return fmt.Errorf("credential %q was not found", target)
	case "signin":
		return errors.New("keys signin is not supported by the selected provider; configure an API credential with keys add")
	default:
		return fmt.Errorf("unknown keys command %q", command)
	}
}

func cliRedact(value string) string {
	if len(value) <= 8 {
		return "********"
	}
	return value[:4] + "..." + value[len(value)-4:]
}

func handleSession(ctx context.Context, args []string, stdout io.Writer) error {
	command := "list"
	if len(args) > 0 {
		command = args[0]
	}
	if command != "list" {
		return fmt.Errorf("unknown session command %q", command)
	}
	db, err := openDefaultStore(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	projects, err := db.ListProjects(ctx)
	if err != nil {
		return err
	}
	type row struct {
		Project  store.Project   `json:"project"`
		Sessions []store.Session `json:"sessions"`
	}
	rows := make([]row, 0, len(projects))
	for _, project := range projects {
		sessions, err := db.ListSessions(ctx, project.ID)
		if err != nil {
			return err
		}
		rows = append(rows, row{Project: project, Sessions: sessions})
	}
	return writeIndented(stdout, rows)
}

func handleProject(ctx context.Context, args []string, stdout io.Writer) error {
	db, err := openDefaultStore(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	if len(args) >= 2 && args[0] == "open" {
		project, err := db.OpenProject(ctx, args[1])
		if err != nil {
			return err
		}
		return writeIndented(stdout, project)
	}
	projects, err := db.ListProjects(ctx)
	if err != nil {
		return err
	}
	return writeIndented(stdout, projects)
}

func handleRun(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	projectPath := fs.String("project", ".", "project directory")
	sessionID := fs.String("session", "", "existing session ID")
	agentID := fs.String("agent", "research", "agent ID")
	providerID := fs.String("provider", "", "provider ID; defaults to persisted settings")
	modelID := fs.String("model", "", "model ID; defaults to persisted settings")
	maxSteps := fs.Int("max-steps", 12, "maximum provider/tool turns")
	timeout := fs.Duration("timeout", 10*time.Minute, "run timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	goal := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if goal == "" {
		return errors.New("run requires a research objective")
	}
	root, err := filepath.Abs(*projectPath)
	if err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("run project is not a directory: %s", root)
	}
	if *maxSteps < 1 {
		return errors.New("run --max-steps must be at least 1")
	}
	if *timeout <= 0 {
		return errors.New("run --timeout must be positive")
	}

	runtimeCtx, stopRuntime := context.WithCancel(ctx)
	instance, err := appruntime.Start(runtimeCtx, appruntime.Options{Port: 0, ProjectRoot: root})
	if err != nil {
		stopRuntime()
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = instance.Close(shutdownCtx)
		stopRuntime()
	}()

	project, err := instance.Store.OpenProject(ctx, root)
	if err != nil {
		return err
	}
	settings, err := instance.Store.ListSettings(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*providerID) == "" {
		*providerID = strings.TrimSpace(settings["defaultProvider"])
	}
	if strings.TrimSpace(*modelID) == "" {
		*modelID = strings.TrimSpace(settings["defaultModel"])
	}
	if strings.TrimSpace(*providerID) == "" {
		return errors.New("run requires a verified provider; configure Settings or pass --provider")
	}
	if strings.TrimSpace(*modelID) == "" {
		return errors.New("run requires a model; configure Settings or pass --model")
	}
	var session store.Session
	if strings.TrimSpace(*sessionID) != "" {
		session, err = instance.Store.GetSession(ctx, strings.TrimSpace(*sessionID))
		if err != nil {
			return fmt.Errorf("run session %q was not found: %w", *sessionID, err)
		}
		if session.ProjectID != project.ID {
			return fmt.Errorf("run session %q does not belong to project %s", session.ID, root)
		}
	} else {
		titleRunes := []rune(goal)
		if len(titleRunes) > 80 {
			titleRunes = titleRunes[:80]
		}
		session, err = instance.Store.CreateSession(ctx, store.CreateSessionInput{ProjectID: project.ID, Title: string(titleRunes), Agent: *agentID})
		if err != nil {
			return err
		}
	}

	payload, err := json.Marshal(map[string]any{
		"goal": goal, "agent": *agentID, "provider": *providerID, "model": *modelID, "maxSteps": *maxSteps,
	})
	if err != nil {
		return err
	}
	requestCtx, cancelRequest := context.WithTimeout(ctx, *timeout)
	defer cancelRequest()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, instance.URL+"/api/sessions/"+session.ID+"/run", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return writeCLIHTTPResponse(stdout, response)
}

func handleExport(ctx context.Context, args []string, stdout io.Writer) error {
	shareID := ""
	outputPath := ""
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--output", "-o":
			if index+1 >= len(args) {
				return errors.New("export --output requires a file path")
			}
			outputPath = args[index+1]
			index++
		default:
			if strings.HasPrefix(args[index], "-") {
				return fmt.Errorf("unknown export option %q", args[index])
			}
			if shareID != "" {
				return errors.New("export accepts exactly one share ID")
			}
			shareID = args[index]
		}
	}
	if strings.TrimSpace(shareID) == "" {
		return errors.New("export requires a share ID")
	}
	db, err := openDefaultStore(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	record, err := db.GetShare(ctx, shareID)
	if err != nil {
		return fmt.Errorf("share %q was not found: %w", shareID, err)
	}
	var bundle sharepkg.Bundle
	if err := json.Unmarshal([]byte(record.Bundle), &bundle); err != nil {
		return fmt.Errorf("share %q contains an invalid bundle: %w", shareID, err)
	}
	if err := validateShareBundle(bundle); err != nil {
		return err
	}
	if outputPath == "" {
		_, err = fmt.Fprintln(stdout, record.Bundle)
		return err
	}
	absolute, err := filepath.Abs(outputPath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(absolute, []byte(record.Bundle+"\n"), 0o600); err != nil {
		return err
	}
	return writeIndented(stdout, map[string]any{"shareId": record.ID, "output": absolute, "bytes": len(record.Bundle)})
}

func handleImport(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return errors.New("import requires exactly one bundle file path")
	}
	file, err := os.Open(args[0])
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("import bundle path is a directory")
	}
	const maxBundleBytes = 50 << 20
	if info.Size() > maxBundleBytes {
		return fmt.Errorf("import bundle exceeds the %d MiB limit", maxBundleBytes>>20)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBundleBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxBundleBytes {
		return fmt.Errorf("import bundle exceeds the %d MiB limit", maxBundleBytes>>20)
	}
	var bundle sharepkg.Bundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return fmt.Errorf("invalid share bundle: %w", err)
	}
	if err := validateShareBundle(bundle); err != nil {
		return err
	}
	for index := range bundle.Artifacts {
		artifact := &bundle.Artifacts[index]
		artifact.ID = cliID("artifact")
		artifact.SessionID = ""
		artifact.CreatedAt = 0
		if artifact.Metadata == "" {
			artifact.Metadata = "{}"
		}
		if !json.Valid([]byte(artifact.Metadata)) {
			return fmt.Errorf("artifact %d metadata is not valid JSON", index+1)
		}
	}
	artifactIDs := make([]string, 0, len(bundle.Artifacts))
	for _, artifact := range bundle.Artifacts {
		artifactIDs = append(artifactIDs, artifact.ID)
	}
	bundleJSON, err := sharepkg.BuildBundle(sharepkg.PublishRequest{Title: bundle.Title, Visibility: bundle.Visibility, ArtifactIDs: artifactIDs}, bundle.Artifacts)
	if err != nil {
		return err
	}
	record := store.Share{
		ID: cliID("share"), Title: bundle.Title, Visibility: bundle.Visibility, Status: "published", Bundle: bundleJSON,
	}
	record.URL = "/api/share/" + record.ID
	db, err := openDefaultStore(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	record, _, err = db.ImportShare(ctx, record, bundle.Artifacts)
	if err != nil {
		return err
	}
	return writeIndented(stdout, record)
}

func validateShareBundle(bundle sharepkg.Bundle) error {
	if bundle.Schema != "superscience.share.v1" {
		return fmt.Errorf("unsupported share bundle schema %q", bundle.Schema)
	}
	if strings.TrimSpace(bundle.Title) == "" {
		return errors.New("share bundle title is required")
	}
	if len(bundle.Artifacts) == 0 {
		return errors.New("share bundle requires at least one artifact")
	}
	return nil
}

func cliID(prefix string) string {
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s_%x", prefix, random)
}

func writeCLIHTTPResponse(stdout io.Writer, response *http.Response) error {
	const maxResponseBytes = 16 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxResponseBytes {
		return errors.New("workbench response exceeds the 16 MiB CLI limit")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		var failure struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &failure) == nil && strings.TrimSpace(failure.Message) != "" {
			if failure.Code != "" {
				return fmt.Errorf("%s: %s", failure.Code, failure.Message)
			}
			return errors.New(failure.Message)
		}
		return fmt.Errorf("workbench returned %s", response.Status)
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("workbench returned invalid JSON: %w", err)
	}
	return writeIndented(stdout, payload)
}

func handleDebugPaths(stdout io.Writer) error {
	dataDir, err := appruntime.DataDir()
	if err != nil {
		return err
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "dataDir=%s\nworkingDir=%s\nfrontend=%s\n", dataDir, wd, filepath.Join(wd, "frontend"))
	return nil
}

func handleDoctor(stdout io.Writer) error {
	dataDir, err := appruntime.DataDir()
	if err != nil {
		return err
	}
	wd, _ := os.Getwd()
	frontend := filepath.Join(wd, "frontend")
	_, frontendErr := os.Stat(frontend)
	fmt.Fprintln(stdout, "Superscience doctor")
	fmt.Fprintf(stdout, "version=%s\n", buildinfo.DisplayVersion())
	fmt.Fprintf(stdout, "dataDir=%s\n", dataDir)
	fmt.Fprintf(stdout, "frontend=%s\n", mapBool(frontendErr == nil, "present", "missing"))
	fmt.Fprintf(stdout, "providers=%d\n", len(providers.BuiltIns()))
	fmt.Fprintf(stdout, "tools=%d\n", len(tools.BuiltIns()))
	return nil
}

func handleCompletion(shell string, stdout io.Writer) error {
	switch shell {
	case "zsh":
		fmt.Fprintln(stdout, "#compdef superscience")
		fmt.Fprintln(stdout, "_superscience() {")
		fmt.Fprintln(stdout, "  local -a commands")
		fmt.Fprintln(stdout, "  commands=(web serve desktop cloud run session models keys agent skill mcp acp project export import generate stats debug doctor completion upgrade uninstall)")
		fmt.Fprintln(stdout, "  _describe 'command' commands")
		fmt.Fprintln(stdout, "}")
	case "bash", "fish":
		fmt.Fprintf(stdout, "# Superscience %s completion: web serve desktop cloud run session models keys agent skill mcp acp project export import generate stats debug doctor completion upgrade uninstall\n", shell)
	default:
		return fmt.Errorf("unsupported completion shell %q", shell)
	}
	return nil
}

func openDefaultStore(ctx context.Context) (*store.Store, error) {
	dataDir, err := appruntime.DataDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	return store.Open(ctx, filepath.Join(dataDir, "superscience.db"))
}

func writeIndented(stdout io.Writer, value any) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	separator := false
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z', character >= '0' && character <= '9', character == '_':
			builder.WriteRune(character)
			separator = false
		case character == ' ', character == '-':
			if builder.Len() > 0 && !separator {
				builder.WriteByte('-')
				separator = true
			}
		}
	}
	value = strings.Trim(builder.String(), "-")
	if value == "" {
		return "custom"
	}
	return value
}

func mapBool(ok bool, yes string, no string) string {
	if ok {
		return yes
	}
	return no
}
