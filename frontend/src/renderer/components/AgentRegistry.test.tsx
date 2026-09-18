import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { RegistryView } from "../lib/registry-api";
import { AgentRegistry } from "./AgentRegistry";

const api = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), versions: vi.fn(), audit: vi.fn(), create: vi.fn(), update: vi.fn(), append: vi.fn(), activate: vi.fn(), clone: vi.fn() }));
vi.mock("../lib/registry-api", () => ({ registryQueryRoot: ["adaptive-registry"], listRegistry: api.list, getRegistry: api.get,
	registryVersions: api.versions, registryAudit: api.audit, createRegistry: api.create, updateRegistry: api.update,
	appendRegistryVersion: api.append, activateRegistryVersion: api.activate, cloneRegistry: api.clone, exportRegistry: vi.fn(), importRegistry: vi.fn() }));
vi.mock("../lib/api-client", () => ({ apiClient: { GET: vi.fn().mockResolvedValue({ data: { supported: [{ id: "codex", label: "Codex" }, { id: "claude-code", label: "Claude Code" }] } }) }, apiErrorMessage: (error: unknown) => error instanceof Error ? error.message : String(error) }));

const view: RegistryView = {
	entry: { id: "coder", kind: "agent_type", origin: "AGENT_MANAGER", createdBy: "manager", metadata: { name: "Coder", description: "Implementation", enabled: true, policy: { managerCanSelect: true, managerCanModify: false, managerCanVersion: false } }, revision: 3, activeVersion: 1, createdAt: "2026-09-18T12:00:00Z", updatedAt: "2026-09-18T12:00:00Z" },
	version: { entryId: "coder", number: 1, parentVersion: 0, definition: { agentType: { harness: "codex", config: {}, instructions: "Check the change", capabilities: [], skills: [], maxParallelWorkers: 2 } }, contentHash: "abc", origin: "AGENT_MANAGER", createdBy: "manager", reason: "Create coder", createdAt: "2026-09-18T12:00:00Z" },
};

function mount() {
	const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
	return render(<QueryClientProvider client={client}><AgentRegistry /></QueryClientProvider>);
}

beforeEach(() => {
	vi.clearAllMocks();
	api.list.mockImplementation(async (kind: string) => ({ items: kind === "agent_type" ? [view] : [] }));
	api.get.mockResolvedValue(view);
	api.versions.mockResolvedValue({ versions: [view.version] });
	api.audit.mockResolvedValue({ events: [] });
	api.create.mockResolvedValue(view);
	api.append.mockResolvedValue({ ...view.version, number: 2 });
	api.update.mockResolvedValue(view.entry);
});

describe("AgentRegistry", () => {
	it("creates a typed configuration with explicit ownership permissions", async () => {
		const user = userEvent.setup();
		mount();
		await user.click(screen.getByRole("button", { name: "Create Agent Type" }));
		expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
		await user.type(screen.getByLabelText("Name", { exact: true }), "Rust Specialist");
		await screen.findByRole("option", { name: "Codex" });
		await user.selectOptions(screen.getByLabelText("Harness"), "codex");
		await user.type(screen.getByLabelText("Instructions"), "Check memory safety");
		await user.type(screen.getByLabelText("Capabilities (comma separated)"), "rust, security");
		await user.type(screen.getByLabelText("Reason for this change"), "Specialize implementation");
		await user.click(screen.getByRole("button", { name: "Save" }));
		await waitFor(() => expect(api.create).toHaveBeenCalledWith("agent_type", expect.objectContaining({ metadata: expect.objectContaining({ name: "Rust Specialist", policy: { managerCanSelect: true, managerCanModify: false, managerCanVersion: false } }), definition: { agentType: expect.objectContaining({ harness: "codex", instructions: "Check memory safety", capabilities: ["rust", "security"], skills: [] }) } })));
	});

	it("offers authored Skills through the same registry", async () => {
		const user = userEvent.setup();
		mount();
		await user.click(screen.getByRole("button", { name: "Skills" }));
		await user.click(screen.getByRole("button", { name: "Create Skill" }));
		await user.type(screen.getByLabelText("Name", { exact: true }), "Review practice");
		await user.type(screen.getByLabelText("Instructions"), "Inspect error handling");
		await user.type(screen.getByLabelText("Required tools (one per line)"), "git");
		await user.click(screen.getByRole("button", { name: "Add resource" }));
		await user.type(screen.getByLabelText("Resource path 1"), "references/review.md");
		await user.type(screen.getByLabelText("Resource content 1"), "Inspect changed files");
		await user.type(screen.getByLabelText("Reason for this change"), "Reusable review");
		await user.click(screen.getByRole("button", { name: "Save" }));
		await waitFor(() => expect(api.create).toHaveBeenCalledWith("skill", expect.objectContaining({ definition: { skill: expect.objectContaining({ instructions: "Inspect error handling", requiredTools: ["git"], resources: [{path: "references/review.md", content: "Inspect changed files"}] }) } })));
	});

	it("retains a draft and reports a stale revision instead of overwriting", async () => {
		api.append.mockRejectedValue(new Error("Definition changed; reload before saving"));
		const user = userEvent.setup();
		mount();
		await user.click(await screen.findByRole("button", { name: /Coder/ }));
		await user.click(await screen.findByRole("button", { name: "New version" }));
		await user.clear(screen.getByLabelText("Instructions"));
		await user.type(screen.getByLabelText("Instructions"), "Keep my draft");
		await user.type(screen.getByLabelText("Reason for this change"), "Improve evidence");
		await user.click(screen.getByRole("button", { name: "Save" }));
		expect(await screen.findByRole("alert")).toHaveTextContent("Definition changed; reload before saving");
		expect(screen.getByLabelText("Instructions")).toHaveValue("Keep my draft");
		expect(api.append).toHaveBeenCalledWith("agent_type", "coder", expect.objectContaining({ expectedRevision: 3 }));
		expect(api.activate).not.toHaveBeenCalled();
	});

	it("shows a recoverable load failure", async () => {
		api.list.mockRejectedValue(new Error("Daemon unavailable"));
		mount();
		expect(await screen.findByRole("alert")).toHaveTextContent("Daemon unavailable");
		expect(screen.getByRole("button", { name: "Retry" })).toBeEnabled();
	});
});
