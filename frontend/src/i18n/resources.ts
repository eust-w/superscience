export type Locale = "en" | "zh-CN";

export const resources = {
  en: {
    onboarding: {
      eyebrow: "Production scientific AI",
      title: "Configure a cloud provider",
      intro: "AI chat requires a provider and model to complete an authenticated verification request. Local research tools are available separately.",
      providerStep: "Provider",
      providerStepDetail: "Select a supported cloud endpoint.",
      verifyStep: "Verify credentials",
      verifyStepDetail: "Run a live model request.",
      systemStep: "System check",
      systemStepDetail: "Store the verified model selection.",
      workspaceStep: "Research workspace",
      workspaceStepDetail: "Add verified AI to your local projects.",
      security: "Keys are sent only to the selected provider and stored through the operating system credential service.",
      requiredSetup: "Required for AI chat",
      connection: "Provider connection",
      required: "Required",
      provider: "Provider",
      endpoint: "Endpoint",
      endpointPlaceholder: "Provider base URL (optional)",
      model: "Model",
      modelPlaceholder: "Select or enter a model ID",
      apiKey: "API key",
      keyPlaceholder: "Stored after successful verification",
      verified: "Provider verified",
      failed: "Verification failed",
      noGeneration: "AI chat remains unavailable until this check succeeds.",
      continue: "Verify and continue",
      localTools: "Open local tools",
      localToolsDetail: "Files, scientific search, and notebooks can be used without an AI provider."
    }
  },
  "zh-CN": {
    onboarding: {
      eyebrow: "生产级科研智能",
      title: "配置云端模型服务",
      intro: "AI 对话需要模型服务与模型通过真实鉴权验证。本地科研工具可以独立使用。",
      providerStep: "模型服务",
      providerStepDetail: "选择受支持的云端服务。",
      verifyStep: "验证凭据",
      verifyStepDetail: "执行一次真实的最小模型请求。",
      systemStep: "系统检查",
      systemStepDetail: "保存已验证的模型选择。",
      workspaceStep: "科研工作区",
      workspaceStepDetail: "为本地项目接入已验证的 AI。",
      security: "密钥仅发送给所选模型服务，并通过操作系统凭据服务存储。",
      requiredSetup: "AI 对话所需配置",
      connection: "模型服务连接",
      required: "必填",
      provider: "模型服务",
      endpoint: "服务地址",
      endpointPlaceholder: "模型服务基础 URL（可选）",
      model: "模型",
      modelPlaceholder: "选择或输入模型 ID",
      apiKey: "API 密钥",
      keyPlaceholder: "验证成功后安全存储",
      verified: "模型服务已验证",
      failed: "验证失败",
      noGeneration: "验证成功前，AI 对话保持不可用。",
      continue: "验证并继续",
      localTools: "打开本地工具",
      localToolsDetail: "无需配置 AI 服务，也可使用文件、科学检索和 Notebook。"
    }
  }
} as const;

export function loadLocale(): Locale {
  if (typeof window === "undefined") return "en";
  return window.localStorage.getItem("superscience.locale") === "zh-CN" ? "zh-CN" : "en";
}

export function saveLocale(locale: Locale) {
  if (typeof window !== "undefined") window.localStorage.setItem("superscience.locale", locale);
}
