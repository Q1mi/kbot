// 后端 domain 类型的前端镜像，使用手写类型并由 TypeScript 构建做契约检查。

export interface Workspace {
  id: string
  name: string
  description: string
  parent_id?: string | null
  created_at: string
  updated_at: string
}

export interface Agent {
  id: string
  workspace_id: string
  name: string
  template: string
  created_by: string
  created_at: string
  updated_at: string
}

export interface CreateAgentRequest {
  name: string
  template: string
  system_prompt?: string
  system_prompt_version_id?: string
  system_prompt_id?: string
  user_prompt_id?: string
  prompt_env?: string
  tool_ids?: string[]
  skill_version_ids?: string[]
  kb_ids?: string[]
  allow_network?: boolean
  max_steps?: number
}

export interface AgentVersionConfig {
  system_prompt?: string
  system_prompt_version_id?: string
  system_prompt_id?: string
  user_prompt_id?: string
  prompt_env?: string
  tool_ids?: string[]
  skill_version_ids?: string[]
  kb_ids?: string[]
  allow_network?: boolean
  max_steps?: number
}

export interface AgentVersion {
  id: string
  agent_id: string
  version: number
  config: AgentVersionConfig
  environments: string[]
  created_by: string
  created_at: string
}

export interface TeamMember {
  agent_id: string
  agent_version_id?: string
  role: string
}

export interface Team {
  id: string
  workspace_id: string
  name: string
  mode: string
  created_at: string
}

export interface CreateTeamRequest {
  name: string
  mode: string
  members: TeamMember[]
}

export interface TeamVersion {
  id: string
  team_id: string
  version: number
  members: TeamMember[]
  environments: string[]
  created_at: string
}

export interface Message {
  id?: string
  conversation_id?: string
  role: string
  content: string
  created_at?: string
}

export interface Conversation {
  id: string
  agent_id: string
  agent_version_id: string
  workspace_id: string
  user_id: string
  status: string
  classification?: string
  started_at?: string
  updated_at?: string
}

// ---- Prompt 中心 ----
export interface Prompt {
  id: string
  workspace_id: string
  name: string
  category: string
  created_by: string
  created_at: string
  updated_at: string
}

export interface PromptVersion {
  id: string
  prompt_id: string
  version: number
  template: string
  variables_schema: string
  model_profile_version_id?: string
  generation_config?: {
    temperature?: number
    top_p?: number
    max_output_tokens?: number
    stop?: string[]
    seed?: number
  }
  hash: string
  token_estimate: number
  created_by: string
  created_at: string
}

// ---- Skills ----
export interface Skill {
  id: string
  workspace_id: string
  name: string
  category: string
  created_by: string
  created_at: string
  updated_at: string
}

export interface SkillVersion {
  id: string
  skill_id: string
  version: number
  frontmatter_json: string
  body_md: string
  status: string
  created_by: string
  created_at: string
}

// ---- Tools ----
export interface Tool {
  id: string
  workspace_id: string
  name: string
  source_type: string
  description: string
  sensitive: boolean
  created_by: string
  created_at: string
  updated_at: string
}

export interface ToolTestRun {
  id: string
  tool_id: string
  tool_version_id: string
  input: string
  output: string
  status: string
  latency_ms: number
  error?: string | null
  created_at: string
}

export interface ToolVersion {
  id: string
  tool_id: string
  version: number
  schema_json: string
  endpoint_config: string
  auth_config: string
  has_auth?: boolean
  retry_policy: string
  status: string
  created_by: string
  created_at: string
}

// ---- Knowledge Base ----
export interface KnowledgeBase {
  id: string
  workspace_id: string
  name: string
  embedding_model: string
  status: string
  created_by: string
  created_at: string
  updated_at: string
}

export interface KbDocument {
  id: string
  kb_id: string
  source_type: string
  source_uri: string
  hash: string
  classification: string
  status: string
  ingested_at?: string | null
  created_at: string
}

export interface KbIngestJob {
  id: string
  kb_id: string
  doc_id: string
  stage: string
  retries: number
  error?: string | null
  started_at: string
  finished_at?: string | null
}

export interface ConnectorInstance {
  id: string
  kb_id: string
  connector_kind: string
  config_json: string
  cursor: string
  last_sync_at?: string | null
  created_at: string
}

export interface Passage {
  doc_id: string
  chunk_id: string
  text: string
  score: number
}

// ---- Eval ----
export interface EvalDataset {
  id: string
  workspace_id: string
  name: string
  target_kind: string
  created_at: string
}

export interface EvalCase {
  id: string
  dataset_id: string
  input: string
  expected: string
  metadata: string
  created_at: string
}

export interface EvalRun {
  id: string
  dataset_id: string
  target_id: string
  judge_id: string
  status: string
  pass_rate: number
  threshold: number
  created_at: string
  finished_at?: string
}

export interface EvalScore {
  run_id: string
  case_id: string
  dimension: string
  score: number
  reason: string
}

export interface EvalRunResult {
  run_id: string
  pass_rate: number
  passed: boolean
  total: number
  scores: EvalScore[]
}

export interface EvalRunHistory {
  run: EvalRun
  scores: EvalScore[]
}

// ---- Audit / Guard ----
export interface AuditLog {
  id: string
  actor: string
  action: string
  resource_type: string
  resource_id: string
  before_json?: string | null
  after_json?: string | null
  ip?: string | null
  user_agent?: string | null
  created_at: string
}

export interface GuardRule {
  id: string
  kind: string
  hook: string
  pattern_or_model: string
  action: string
  enabled: boolean
  workspace_id: string
}

export interface GuardQuota {
  metric: string
  period: string
  used: number
  limit: number
}

// ---- Approval ----
export interface Approval {
  id: string
  conversation_id: string
  action: string
  payload: string
  status: string
  approver_id?: string
  created_at: string
  resolved_at?: string | null
}
