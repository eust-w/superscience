export type Locale = "en" | "zh-CN";

export const resources = {
  en: {
    onboarding: {
      eyebrow: "Production scientific AI",
      title: "Configure a cloud provider",
      intro: "Superscience runs only after a provider and model complete an authenticated verification request.",
      providerStep: "Provider",
      providerStepDetail: "Select a supported cloud endpoint.",
      verifyStep: "Verify credentials",
      verifyStepDetail: "Run a live model request.",
      systemStep: "System check",
      systemStepDetail: "Store the verified model selection.",
      workspaceStep: "Research workspace",
      workspaceStepDetail: "Open projects after verification.",
      security: "Keys are sent only to the selected provider and stored through the operating system credential service.",
      requiredSetup: "Required setup",
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
      noGeneration: "No research content is generated until this check succeeds.",
      continue: "Verify and continue"
    }
  },
  "zh-CN": {
    onboarding: {
      eyebrow: "生产级科研智能",
      title: "配置云端模型服务",
      intro: "Superscience 仅在模型服务与模型完成真实鉴权请求后才会进入科研工作区。",
      providerStep: "模型服务",
      providerStepDetail: "选择受支持的云端服务。",
      verifyStep: "验证凭据",
      verifyStepDetail: "执行一次真实的最小模型请求。",
      systemStep: "系统检查",
      systemStepDetail: "保存已验证的模型选择。",
      workspaceStep: "科研工作区",
      workspaceStepDetail: "验证成功后打开本地项目。",
      security: "密钥仅发送给所选模型服务，并通过操作系统凭据服务存储。",
      requiredSetup: "必需配置",
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
      noGeneration: "检查成功前不会生成任何科研内容。",
      continue: "验证并继续"
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
