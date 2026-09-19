import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { KnowledgeView } from "./KnowledgeView";

const api = vi.hoisted(() => ({
	list: vi.fn(),
	versions: vi.fn(),
	create: vi.fn(),
	revise: vi.fn(),
}));
vi.mock("../lib/adaptive-api", () => ({
	knowledgeQueryRoot: ["project-knowledge"],
	listProjectKnowledge: api.list,
	listKnowledgeVersions: api.versions,
	createProjectKnowledge: api.create,
	reviseProjectKnowledge: api.revise,
}));
vi.mock("../lib/api-client", () => ({ apiErrorMessage: (error: unknown) => error instanceof Error ? error.message : String(error) }));

function mount() {
	const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={client}>
			<KnowledgeView projectId="proj" />
		</QueryClientProvider>,
	);
}

function definition(overrides: Record<string, unknown> = {}) {
	return {
		classification: "technical",
		confidence: "high",
		content: "Services communicate through the review queue only.",
		engagementId: "engagement-1",
		kind: "architecture",
		pinned: true,
		sources: [],
		status: "accepted",
		taskIds: ["task-1"],
		tags: ["backend"],
		title: "Review queue is the only service boundary",
		...overrides,
	};
}

function entry(overrides: Record<string, unknown> = {}) {
	return {
		knowledge: { id: "know-1", projectId: "proj", version: 3, createdAt: "2026-09-10T08:00:00.000Z", updatedAt: "2026-09-15T09:00:00.000Z" },
		version: { knowledgeId: "know-1", number: 3, definition: definition(), contentHash: "abc123", actor: { id: "local-user", kind: "USER" }, reason: "Confirmed after the incident review", createdAt: "2026-09-15T09:00:00.000Z" },
		...overrides,
	};
}

beforeEach(() => {
	vi.clearAllMocks();
	api.list.mockResolvedValue({ items: [entry()], nextCursor: "" });
	api.versions.mockResolvedValue({
		items: [
			{ knowledgeId: "know-1", number: 3, definition: definition(), contentHash: "abc123", actor: { id: "local-user", kind: "USER" }, reason: "Confirmed after the incident review", createdAt: "2026-09-15T09:00:00.000Z" },
			{ knowledgeId: "know-1", number: 2, definition: definition({ status: "candidate" }), contentHash: "def456", actor: { id: "session-4", kind: "WORKER" }, reason: "Recorded from the failed deploy", createdAt: "2026-09-11T08:00:00.000Z" },
		],
		nextCursor: "",
	});
	api.create.mockResolvedValue(entry());
	api.revise.mockResolvedValue({ knowledgeId: "know-1", number: 4, definition: definition(), contentHash: "xyz789", actor: { id: "local-user", kind: "USER" }, reason: "Tightened wording", createdAt: "2026-09-19T09:00:00.000Z" });
});

describe("KnowledgeView", () => {
	it("lists project knowledge with status, kind and version facts", async () => {
		mount();
		expect(await screen.findByText("Review queue is the only service boundary")).toBeInTheDocument();
		expect(screen.getAllByText("accepted").length).toBeGreaterThan(0);
		expect(screen.getAllByText("architecture").length).toBeGreaterThan(0);
	});

	it("inspects one entry with provenance and immutable history", async () => {
		mount();
		await userEvent.click(await screen.findByText("Review queue is the only service boundary"));
		expect(await screen.findByText(/USER:local-user — Confirmed after the incident review/)).toBeInTheDocument();
		expect(screen.getByText("WORKER:session-4 — Recorded from the failed deploy")).toBeInTheDocument();
		expect(screen.getByText("abc123")).toBeInTheDocument();
		expect(screen.getByText("Pinned")).toBeInTheDocument();
		expect(screen.getByText(/backend/)).toBeInTheDocument();
	});

	it("records new knowledge with its reason and selects the created entry", async () => {
		mount();
		await userEvent.click(await screen.findByRole("button", { name: "New knowledge" }));
		await userEvent.type(screen.getByLabelText("Title"), "Deploys require green CI");
		await userEvent.type(screen.getByLabelText("Content"), "Never merge with a red check.");
		await userEvent.type(screen.getByLabelText(/Reason/), "Postmortem action");
		await userEvent.click(screen.getByRole("button", { name: "Record knowledge" }));
		await waitFor(() => expect(api.create).toHaveBeenCalledWith("proj", {
			definition: expect.objectContaining({ title: "Deploys require green CI", content: "Never merge with a red check.", kind: "architecture", status: "candidate" }),
			reason: "Postmortem action",
		}));
	});

	it("revises through the optimistic version fence", async () => {
		mount();
		await userEvent.click(await screen.findByText("Review queue is the only service boundary"));
		await userEvent.click(await screen.findByRole("button", { name: "Revise" }));
		const content = screen.getByLabelText("Content");
		await userEvent.clear(content);
		await userEvent.type(content, "Updated boundary description.");
		await userEvent.type(screen.getByLabelText(/Reason/), "Tightened wording");
		await userEvent.click(screen.getByRole("button", { name: "Save revision" }));
		await waitFor(() => expect(api.revise).toHaveBeenCalledWith("know-1", {
			definition: expect.objectContaining({ content: "Updated boundary description.", pinned: true, status: "accepted", classification: "technical" }),
			expectedVersion: 3,
			reason: "Tightened wording",
		}));
	});

	it("surfaces a revision conflict without losing the draft", async () => {
		api.revise.mockRejectedValue(new Error("Knowledge changed; reload its current version"));
		mount();
		await userEvent.click(await screen.findByText("Review queue is the only service boundary"));
		await userEvent.click(await screen.findByRole("button", { name: "Revise" }));
		await userEvent.type(screen.getByLabelText(/Reason/), "Retry after reload");
		await userEvent.click(screen.getByRole("button", { name: "Save revision" }));
		expect(await screen.findByRole("alert")).toHaveTextContent(/Knowledge changed/);
		expect(screen.getByRole("button", { name: "Save revision" })).toBeInTheDocument();
	});

	it("recovers from a listing failure", async () => {
		api.list.mockRejectedValueOnce(new Error("daemon unreachable"));
		mount();
		expect(await screen.findByRole("alert")).toHaveTextContent(/daemon unreachable/);
		api.list.mockResolvedValue({ items: [entry()], nextCursor: "" });
		await userEvent.click(screen.getByRole("button", { name: "Retry" }));
		expect(await screen.findByText("Review queue is the only service boundary")).toBeInTheDocument();
	});

	it("reports an empty project honestly", async () => {
		api.list.mockResolvedValue({ items: [], nextCursor: "" });
		mount();
		expect(await screen.findByText(/No knowledge recorded for this project yet/i)).toBeInTheDocument();
	});
});
