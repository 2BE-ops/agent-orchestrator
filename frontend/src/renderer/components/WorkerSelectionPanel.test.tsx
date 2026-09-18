import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { beforeEach, expect, it, vi } from "vitest";
import type {
	RegistryVersion,
	RegistryView,
	WorkerSelection,
} from "../lib/registry-api";
import { WorkerSelectionPanel } from "./WorkerSelectionPanel";

const api = vi.hoisted(() => ({
	list: vi.fn(),
	version: vi.fn(),
	versions: vi.fn(),
	get: vi.fn(),
	changed: vi.fn(),
}));
vi.mock("../lib/registry-api", () => ({
	registryQueryRoot: ["adaptive-registry"],
	listRegistry: api.list,
	getRegistryVersion: api.version,
	registryVersions: api.versions,
}));
vi.mock("../lib/api-client", () => ({
	apiClient: { GET: api.get },
	apiErrorMessage: (error: unknown) =>
		error instanceof Error ? error.message : String(error),
}));

const definition: RegistryVersion = {
	entryId: "reviewer",
	number: 2,
	parentVersion: 1,
	definition: {
		agentType: {
			harness: "codex",
			config: { model: "review-model" },
			sessionMode: "tui",
			instructions: "Pinned instructions",
			skills: [{ id: "retained-skill", version: 1 }],
			maxParallelWorkers: 2,
			capabilities: [],
		},
	},
	contentHash: "abc",
	origin: "USER",
	createdBy: "human",
	reason: "Reviewed",
	createdAt: "2026-09-18T00:00:00Z",
};
const view: RegistryView = {
	entry: {
		id: "reviewer",
		kind: "agent_type",
		origin: "USER",
		createdBy: "human",
		metadata: {
			name: "Reviewer",
			enabled: true,
			description: "",
			policy: {
				managerCanSelect: false,
				managerCanModify: false,
				managerCanVersion: false,
			},
		},
		revision: 2,
		activeVersion: 2,
		createdAt: "2026-09-18T00:00:00Z",
		updatedAt: "2026-09-18T00:00:00Z",
	},
	version: definition,
};

function mount(initial?: WorkerSelection) {
	function Form() {
		const [selection, setSelection] = useState(initial);
		return (
			<WorkerSelectionPanel
				projectId="project"
				selection={selection}
				onChange={(value) => {
					setSelection(value);
					api.changed(value);
				}}
			/>
		);
	}
	return render(
		<QueryClientProvider
			client={
				new QueryClient({ defaultOptions: { queries: { retry: false } } })
			}
		>
			<Form />
		</QueryClientProvider>,
	);
}
beforeEach(() => {
	vi.resetAllMocks();
	api.list.mockImplementation(async (kind: string) => ({
		items: kind === "agent_type" ? [view] : [],
	}));
	api.versions.mockResolvedValue({ versions: [definition] });
	api.version.mockResolvedValue(definition);
	api.get.mockImplementation(async (path: string) => ({
		data: path.endsWith("/configuration")
			? {
					fields: [{ key: "model" }, { key: "permissions", options: ["auto"] }],
					sessionModes: ["tui", "chat"],
				}
			: {
					models: [{ id: "review-model", label: "Review model" }],
					selectionMode: "catalog",
					customModelEntry: "configured",
				},
	}));
});

it("pins the active version once and retains explicit empty instructions and Skills", async () => {
	const user = userEvent.setup();
	mount();
	await screen.findByRole("option", { name: "Reviewer · v2" });
	await user.selectOptions(
		await screen.findByLabelText("Worker configuration"),
		"reviewer",
	);
	await waitFor(() =>
		expect(api.version).toHaveBeenCalledWith("agent_type", "reviewer", 2),
	);
	await user.click(screen.getByText("One-off overrides"));
	await user.click(screen.getByLabelText("Override instructions"));
	await user.clear(screen.getByLabelText("Worker instructions"));
	await user.click(screen.getByLabelText("Change Skills for this worker"));
	await user.click(
		screen.getByRole("button", { name: "Remove Skill retained-skill" }),
	);
	expect(api.changed).toHaveBeenLastCalledWith({
		agentTypeId: "reviewer",
		version: 2,
		overrides: { instructions: "", skills: [] },
	});
	expect(definition.definition.agentType?.instructions).toBe(
		"Pinned instructions",
	);
	expect(definition.definition.agentType?.skills).toEqual([
		{ id: "retained-skill", version: 1 },
	]);
});

it("offers additional pinned versions and reports failed exact-version loads", async () => {
	const user = userEvent.setup();
	api.versions.mockImplementation(
		async (_kind: string, _id: string, cursor: string) =>
			cursor
				? { versions: [{ ...definition, number: 1 }] }
				: { versions: [definition], nextCursor: "older" },
	);
	mount({ agentTypeId: "reviewer", version: 2, overrides: {} });
	await user.click(
		await screen.findByRole("button", { name: "More versions" }),
	);
	await screen.findByRole("option", { name: "v1 · Reviewed" });
	api.version.mockRejectedValueOnce(new Error("Version is unavailable"));
	await user.selectOptions(
		screen.getByLabelText("Pinned Agent Type version"),
		"1",
	);
	expect(await screen.findByRole("alert")).toHaveTextContent(
		"Version is unavailable",
	);
	expect(api.changed).toHaveBeenLastCalledWith({
		agentTypeId: "reviewer",
		version: 1,
		overrides: {},
	});
});

it("uses native mode choices for a harness with modes instead of model IDs", async () => {
	const user = userEvent.setup();
	api.get.mockImplementation(async (path: string) => ({
		data: path.endsWith("/configuration")
			? { fields: [{ key: "mode" }], sessionModes: ["tui"] }
			: { models: [{ id: "smart", label: "Smart" }], selectionMode: "mode" },
	}));
	mount({ agentTypeId: "reviewer", version: 2, overrides: {} });
	await user.click(await screen.findByText("One-off overrides"));
	await user.selectOptions(await screen.findByLabelText("Agent mode"), "smart");
	expect(api.changed).toHaveBeenLastCalledWith({
		agentTypeId: "reviewer",
		version: 2,
		overrides: { mode: "smart", model: undefined, effort: undefined },
	});
	expect(screen.queryByLabelText("Override model")).not.toBeInTheDocument();
});
