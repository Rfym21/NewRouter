// Codex request simulation. A channel binds an instance of this plugin and
// every Responses request through that channel gets the Codex CLI fingerprint
// unless it already comes from Codex itself.
const DEFAULT_SESSION_ID = "01a1dec3-6e18-7193-a48f-e1c4667583c5";
const DEFAULT_TURN_ID = "01a1dec3-6e31-77a2-8afe-c49d875185a6";
const DEFAULT_CONTEXT_WINDOW_ID = "01a1dec3-6e1c-76c1-be3e-1befdfc5c5a7";
const DEFAULT_INSTALLATION_ID = "39faaa95-1437-46d2-8f53-100e5d157bfb";
const DEFAULT_METADATA = {
  installation_id: DEFAULT_INSTALLATION_ID,
  session_id: DEFAULT_SESSION_ID,
  thread_id: DEFAULT_SESSION_ID,
  turn_id: DEFAULT_TURN_ID,
  root_turn_id: DEFAULT_TURN_ID,
  window_id: DEFAULT_SESSION_ID + ":0",
  window_number: 0,
  context_window_id: DEFAULT_CONTEXT_WINDOW_ID,
  agent_name: "/root",
  request_kind: "turn",
  history_initialization: "new",
  thread_source: "user",
  turn_trigger: "user",
  sandbox: "none",
  sandbox_mode: "read-only",
  auto_review_enabled: false,
  node_repl_auto_review_required: false,
  node_repl_disabled: false,
  turn_started_at_unix_ms: 1794740237086,
  analytics_enabled: true,
};
const DEFAULT_HEADERS = {
  accept: "{accept}",
  originator: "codex-tui",
  "user-agent":
    "codex-tui/0.167.0 (Windows 10.0.26340; x86_64) WindowsTerminal (codex-tui; 0.167.0)",
  "session-id": "{session_id}",
  "thread-id": "{thread_id}",
  "x-client-request-id": "{thread_id}",
  "x-codex-window-id": "{window_id}",
  "x-codex-turn-metadata": "{turn_metadata}",
  "x-codex-beta-features": "remote_compaction_v2",
};
const DEFAULT_SYSTEM = [
  {
    type: "message",
    role: "developer",
    content: [
      {
        type: "input_text",
        text: "You are Codex, a coding agent based on GPT-5. You and the user share one workspace, and your job is to collaborate with them until their goal is genuinely handled.",
      },
    ],
  },
];

const PROTECTED_HEADERS = {
  authorization: true,
  "proxy-authorization": true,
  cookie: true,
  host: true,
  "content-length": true,
  "content-type": true,
  "content-encoding": true,
  connection: true,
  "keep-alive": true,
  "proxy-connection": true,
  "transfer-encoding": true,
  upgrade: true,
  "x-api-key": true,
  "api-key": true,
  "x-goog-api-key": true,
};

function isObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function has(object, key) {
  return Object.prototype.hasOwnProperty.call(object, key);
}

function text(value) {
  return typeof value === "string" ? value : "";
}

function mergeComma() {
  const parts = [];
  const seen = {};
  for (let i = 0; i < arguments.length; i++) {
    const value = arguments[i];
    if (typeof value !== "string") continue;
    const items = value.split(",");
    for (const item of items) {
      const part = item.trim();
      if (part === "") continue;
      const key = part.toLowerCase();
      if (seen[key]) continue;
      seen[key] = true;
      parts.push(part);
    }
  }
  return parts.join(",");
}

function isRealClient(ctx) {
  return (
    ctx.client &&
    ctx.client.kind === "codex" &&
    ctx.config.preserve_real_client !== false
  );
}

function simulatedMetadata(shared, body, ctx) {
  const metadata = {};
  const defaults = DEFAULT_METADATA;
  for (const key of Object.keys(defaults)) metadata[key] = defaults[key];
  if (isObject(shared.metadata)) {
    for (const key of Object.keys(shared.metadata)) metadata[key] = shared.metadata[key];
  }
  if (isObject(body) && isObject(body.client_metadata)) {
    const client = body.client_metadata;
    if (typeof client.session_id === "string" && client.session_id.trim() !== "") metadata.session_id = client.session_id;
    if (typeof client.thread_id === "string" && client.thread_id.trim() !== "") metadata.thread_id = client.thread_id;
    if (typeof client.turn_id === "string" && client.turn_id.trim() !== "") {
      metadata.turn_id = client.turn_id;
      metadata.root_turn_id = client.turn_id;
    }
    if (typeof client["x-codex-installation-id"] === "string" && client["x-codex-installation-id"].trim() !== "") {
      metadata.installation_id = client["x-codex-installation-id"];
    }
    if (typeof client["x-codex-window-id"] === "string" && client["x-codex-window-id"].trim() !== "") {
      metadata.window_id = client["x-codex-window-id"];
    }
  }
  if (text(metadata.session_id).trim() === "") metadata.session_id = DEFAULT_SESSION_ID;
  if (text(metadata.thread_id).trim() === "") metadata.thread_id = metadata.session_id;
  if (text(metadata.window_id).trim() === "") metadata.window_id = metadata.session_id + ":0";
  if (text(metadata.turn_id).trim() === "") metadata.turn_id = DEFAULT_TURN_ID;
  if (text(metadata.root_turn_id).trim() === "") metadata.root_turn_id = metadata.turn_id;
  if (text(metadata.installation_id).trim() === "") metadata.installation_id = DEFAULT_INSTALLATION_ID;
  if (!has(metadata, "model") && ctx.upstreamModel) metadata.model = ctx.upstreamModel;
  if (
    !has(metadata, "reasoning_effort") &&
    isObject(body) &&
    isObject(body.reasoning) &&
    typeof body.reasoning.effort === "string"
  ) {
    metadata.reasoning_effort = body.reasoning.effort;
  }
  return metadata;
}

function clientMetadata(metadata) {
  return {
    "x-codex-installation-id": text(metadata.installation_id),
    session_id: text(metadata.session_id),
    thread_id: text(metadata.thread_id),
    "x-codex-window-id": text(metadata.window_id),
    turn_id: text(metadata.turn_id),
    "x-codex-turn-metadata": JSON.stringify(metadata),
  };
}

function validClientMetadata(value) {
  return isObject(value) && typeof value.session_id === "string" && value.session_id.trim() !== "";
}

function headerEffects(ctx, req, metadata) {
  const clientHeaders = (ctx.client && ctx.client.headers) || {};
  if (isRealClient(ctx)) {
    const set = {};
    for (const name of Object.keys(clientHeaders)) {
      if (PROTECTED_HEADERS[name.toLowerCase()]) continue;
      set[name] = clientHeaders[name];
    }
    return Object.keys(set).length ? [{ type: "header.patch", set: set }] : [];
  }
  const shared = ctx.shared || {};
  const headers = isObject(shared.headers) ? shared.headers : DEFAULT_HEADERS;
  const replacements = {
    "{accept}": ctx.stream ? "text/event-stream" : "application/json",
    "{session_id}": text(metadata.session_id),
    "{thread_id}": text(metadata.thread_id),
    "{installation_id}": text(metadata.installation_id),
    "{window_id}": text(metadata.window_id),
    "{turn_id}": text(metadata.turn_id),
    "{turn_metadata}": JSON.stringify(metadata),
  };
  const set = {};
  for (const name of Object.keys(headers)) {
    const lower = name.toLowerCase();
    if (PROTECTED_HEADERS[lower]) continue;
    let value = String(headers[name]);
    for (const placeholder of Object.keys(replacements)) {
      value = value.split(placeholder).join(replacements[placeholder]);
    }
    if (lower === "x-codex-beta-features") {
      let earlier = "";
      if (req.headers) {
        for (const key of Object.keys(req.headers)) {
          if (key.toLowerCase() === "x-codex-beta-features") earlier = req.headers[key];
        }
      }
      value = mergeComma(earlier, clientHeaders["x-codex-beta-features"], value);
    }
    set[name] = value;
  }
  return Object.keys(set).length ? [{ type: "header.patch", set: set }] : [];
}

function isCodexDeveloperMessage(item) {
  if (!isObject(item) || item.role !== "developer") return false;
  if (typeof item.content === "string") return item.content.indexOf("You are Codex") >= 0;
  if (Array.isArray(item.content)) {
    for (const part of item.content) {
      if (isObject(part) && typeof part.text === "string" && part.text.indexOf("You are Codex") >= 0) {
        return true;
      }
    }
  }
  return false;
}

function inputPatch(input, blocks) {
  if (input === undefined || input === null) {
    return [{ op: "add", path: "/input", value: blocks }];
  }
  if (Array.isArray(input)) {
    if (input.length > 0 && isCodexDeveloperMessage(input[0])) return [];
    return [{ op: "add", path: "/input", value: blocks.concat(input) }];
  }
  if (typeof input === "string") {
    return [
      {
        op: "add",
        path: "/input",
        value: blocks.concat([
          {
            type: "message",
            role: "user",
            content: [{ type: "input_text", text: input }],
          },
        ]),
      },
    ];
  }
  return [];
}

function bodyEffects(ctx, req, metadata) {
  if (isRealClient(ctx)) return [];
  const shared = ctx.shared || {};
  const body = isObject(req.body) ? req.body : {};
  const ops = [];
  if (ctx.config.inject_client_metadata !== false) {
    if (!validClientMetadata(body.client_metadata)) {
      ops.push({ op: "add", path: "/client_metadata", value: clientMetadata(metadata) });
    }
  }
  if (ctx.config.inject_body_defaults !== false) {
    ops.push({ op: "add", path: "/store", value: false });
    if (ctx.stream && body.stream === undefined) {
      ops.push({ op: "add", path: "/stream", value: true });
    }
    if (text(body.prompt_cache_key).trim() === "") {
      ops.push({ op: "add", path: "/prompt_cache_key", value: text(metadata.session_id) });
    }
    if (!has(body, "parallel_tool_calls")) {
      ops.push({ op: "add", path: "/parallel_tool_calls", value: true });
    }
    if (!has(body, "tool_choice")) {
      ops.push({ op: "add", path: "/tool_choice", value: "auto" });
    }
    if (!has(body, "text")) {
      ops.push({ op: "add", path: "/text", value: { verbosity: "low" } });
    }
    if (!Array.isArray(body.include)) {
      ops.push({ op: "add", path: "/include", value: ["reasoning.encrypted_content"] });
    } else if (body.include.indexOf("reasoning.encrypted_content") < 0) {
      ops.push({ op: "add", path: "/include/-", value: "reasoning.encrypted_content" });
    }
  }
  if (ctx.config.inject_system !== false) {
    const blocks = Array.isArray(shared.system) ? shared.system : DEFAULT_SYSTEM;
    ops.push.apply(ops, inputPatch(body.input, blocks));
  }
  return ops.length ? [{ type: "json.patch", ops: ops }] : [];
}

export const meta = {
  apiVersion: 1,
  key: "codex-simulation",
  name: { en: "Codex simulation", zh: "Codex 模拟" },
  description: {
    en: "Adds a Codex CLI fingerprint to Responses requests through a channel: headers, client metadata and developer prompt. A request that already comes from Codex keeps its own fingerprint",
    zh: "为经过渠道的 Responses 请求添加 Codex CLI 指纹：请求头、客户端元数据和开发者提示词。已经来自 Codex 的请求保留自身指纹",
  },
  version: "1.0.0",
  author: { name: "QuantumNous" },
  optional: true,
  configSchema: {
    type: "object",
    properties: {
      preserve_real_client: {
        type: "boolean",
        title: { en: "Keep real Codex requests", zh: "保留真实 Codex 请求" },
        description: {
          en: "Whether a request that already comes from Codex keeps its own headers and body",
          zh: "是否让已经来自 Codex 的请求保留自身的请求头和请求体",
        },
        default: true,
      },
      inject_headers: {
        type: "boolean",
        title: { en: "Add headers", zh: "添加请求头" },
        description: {
          en: "Whether the simulated Codex headers are added",
          zh: "是否添加模拟的 Codex 请求头",
        },
        default: true,
      },
      inject_client_metadata: {
        type: "boolean",
        title: { en: "Add client metadata", zh: "添加客户端元数据" },
        description: {
          en: "Whether client_metadata is filled when the request has none",
          zh: "请求没有 client_metadata 时是否补上",
        },
        default: true,
      },
      inject_body_defaults: {
        type: "boolean",
        title: { en: "Add body defaults", zh: "添加请求体默认值" },
        description: {
          en: "Whether store, prompt_cache_key, tool choice and the other Responses defaults are filled",
          zh: "是否补齐 store、prompt_cache_key、工具选择等 Responses 默认字段",
        },
        default: true,
      },
      inject_system: {
        type: "boolean",
        title: { en: "Add developer prompt", zh: "添加开发者提示词" },
        description: {
          en: "Whether the Codex developer message is prepended when the request has none",
          zh: "请求没有 Codex 开发者消息时是否前置",
        },
        default: true,
      },
    },
  },
  sharedConfigSchema: {
    type: "object",
    properties: {
      headers: {
        type: "object",
        format: "json",
        title: { en: "Simulated headers", zh: "模拟请求头" },
        description: {
          en: "Headers sent upstream; session, thread, window and turn placeholders are filled per request",
          zh: "发送给上游的请求头；session、thread、window 和 turn 占位符会按请求填充",
        },
        default: DEFAULT_HEADERS,
      },
      metadata: {
        type: "object",
        format: "json",
        title: { en: "Simulated metadata", zh: "模拟元数据" },
        description: {
          en: "The Codex client and turn metadata used when the request has none",
          zh: "请求没有时使用的 Codex 客户端与回合元数据",
        },
        default: DEFAULT_METADATA,
      },
      system: {
        type: "array",
        title: { en: "Developer prompt", zh: "开发者提示词" },
        description: {
          en: "The developer messages prepended when the request has no Codex identity",
          zh: "请求没有 Codex 身份时前置的开发者消息",
        },
        items: { type: "object", title: { en: "Developer message", zh: "开发者消息" } },
        default: DEFAULT_SYSTEM,
      },
    },
  },
};

export function outbound(ctx, req) {
  if (ctx.format !== "openai_responses") return { action: "continue" };
  const body = isObject(req.body) ? req.body : {};
  const metadata = simulatedMetadata(ctx.shared || {}, body, ctx);
  const effects = [];
  if (ctx.config.inject_headers !== false) effects.push.apply(effects, headerEffects(ctx, req, metadata));
  effects.push.apply(effects, bodyEffects(ctx, req, metadata));
  return effects.length ? { action: "continue", effects: effects } : { action: "continue" };
}
