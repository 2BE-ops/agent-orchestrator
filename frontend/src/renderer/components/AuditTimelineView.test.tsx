import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AuditTimelineView } from "./AuditTimelineView";

const api = vi.hoisted(() => ({ managerAudit: vi.fn(), taskAudit: vi.fn(), tasks: vi.fn() }));
vi.mock("../lib/adaptive-api", () => ({
	adaptiveAuditQueryRoot: ["adaptive-audit"],
	adaptiveTasksQueryRoot: ["adaptive-tasks"],
	listManagerAudit: api.managerAudit,
	listTaskAudit: api.taskAudit,
	listProjectTasks: api.tasks,
}));
vi.mock("../lib/api-client", () => ({ apiErrorMessage: (error: unknown) => error instanceof Error ? error.message : String(error) }));
const navigateMock = vi.fn();
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigateMock }));

function mount() {
	const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={client}>
			<AuditTimelineView projectId="proj" />
		</QueryClientProvider>,
	);
}

function managerEntry(overrides: Record<string, unknown> = {}) {
	return {
		sequence: 12,
		projectId: "proj",
		configurationVersion: 3,
		action: "configured",
		actor: { id: "local-user", kind: "USER" },
		reason: "Enabled automatic selection",
		createdAt: "2026-09-15T09:00:00.000Z",
		...overrides,
	};
}

function taskAuditEntry(overrides: Record<string, unknown> = {}) {
	return {
		sequence: 40,
		taskId: "task-9",
		revision: 2,
		action: "revised",
		actor: { id: "session-4", kind: "ORCHESTRATOR" },
		reason: "Tightened scope after review",
		createdAt: "2026-09-16T09:00:00.000Z",
		...overrides,
	};
}

function taskView() {
	const actor = { id: "session-4", kind: "ORCHESTRATOR" } as const;
	return {
		task: { id: "task-9", projectId: "proj", revision: 2, createdAt: "2026-09-10T08:00:00Z", updatedAt: "2026-09-16T09:00:00Z", createdBy: { id: "local-user", kind: "USER" } },
		revision: {
			actor,
			contentHash: "hash-task-9",
			createdAt: "2026-09-16T09:00:00Z",
			criteriaVersion: 1,
			number: 2,
			reason: "Tightened scope after review",
			taskId: "task-9",
			definition: { title: "Harden the review queue", brief: "Restrict publish rights", category: "feature", dependencies: null, maxAttempts: 3, parentId: null, priority: 5, requiredCapabilities: null },
		},
		state: { phase: "completed", reason: "verified against frozen criteria", requiresReconciliation: false },
		intent: { actor: { id: "local-user", kind: "USER" }, createdAt: "2026-09-10T08:00:00Z", intent: "run", reason: "Plan approved", taskId: "task-9", taskRevision: 1, version: 1 },
		completion: { taskId: "task-9", taskRevision: 2, reason: "All criteria verified", verified: true },
		criteria: { actor: { id: "local-user", kind: "USER" }, contentHash: "criteria-hash", createdAt: "2026-09-10T08:00:00Z", definition: { criteria: [{ id: "c1", evidenceKind: "test", requirement: "go test passes" }] }, number: 1, previousVersion: 0, reason: "Frozen before work", taskId: "task-9" },
	};
}

beforeEach(() => {
	vi.clearAllMocks();
	api.managerAudit.mockResolvedValue({ items: [managerEntry()], nextCursor: "" });
	api.taskAudit.mockResolvedValue({ items: [taskAuditEntry()], nextCursor: "" });
	api.tasks.mockResolvedValue({ items: [taskView()], nextCursor: "" });
});

describe("AuditTimelineView", () => {
	it("renders the project policy timeline with authors and configuration versions", async () => {
		mount();
		expect(await screen.findByText("configured")).toBeInTheDocument();
		expect(screen.getByText(/Manager configuration v3/)).toBeInTheDocument();
		expect(screen.getByText(/USER:local-user/)).toBeInTheDocument();
		expect(screen.getByText("Enabled automatic selection")).toBeInTheDocument();
	});

	it("reads one task's durable history after picking it", async () => {
		mount();
		await screen.findByText("configured");
		await userEvent.selectOptions(screen.getByLabelText("Task"), "task-9");
		expect(await screen.findByText("revised")).toBeInTheDocument();
		expect(screen.getByText(/Task revision v2/)).toBeInTheDocument();
		expect(screen.getByText(/ORCHESTRATOR:session-4/)).toBeInTheDocument();
		expect(api.taskAudit).toHaveBeenCalledWith("task-9", "");
	});

	it("pages task audit history by cursor", async () => {
		api.taskAudit.mockResolvedValueOnce({ items: [taskAuditEntry()], nextCursor: "cursor-5" });
		mount();
		await screen.findByText("configured");
		await userEvent.selectOptions(screen.getByLabelText("Task"), "task-9");
		await screen.findByText("revised");
		await userEvent.click(screen.getByRole("button", { name: "Load more" }));
		await waitFor(() => expect(api.taskAudit).toHaveBeenNthCalledWith(2, "task-9", "cursor-5"));
	});

	it("recovers from a governance timeline failure", async () => {
		api.managerAudit.mockRejectedValueOnce(new Error("audit store closed"));
		mount();
		expect(await screen.findByRole("alert")).toHaveTextContent(/audit store closed/);
		api.managerAudit.mockResolvedValue({ items: [managerEntry()], nextCursor: "" });
		await userEvent.click(screen.getByRole("button", { name: "Retry" }));
		expect(await screen.findByText("configured")).toBeInTheDocument();
	});

	it("reports empty histories honestly", async () => {
		api.managerAudit.mockResolvedValue({ items: [], nextCursor: "" });
		api.taskAudit.mockResolvedValue({ items: [], nextCursor: "" });
		mount();
		expect(await screen.findByText(/No Manager governance actions recorded yet/i)).toBeInTheDocument();
		await userEvent.selectOptions(await screen.findByLabelText("Task"), "task-9");
		expect(await screen.findByText(/No audit entries recorded for this task/i)).toBeInTheDocument();
	});
});
