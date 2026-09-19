import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ControlCenterView } from "./ControlCenterView";

const api = vi.hoisted(() => ({
	control: vi.fn(),
	setControl: vi.fn(),
	cancelWork: vi.fn(),
	needsHuman: vi.fn(),
	resolve: vi.fn(),
	experiments: vi.fn(),
	recommendations: vi.fn(),
}));
vi.mock("../lib/adaptive-api", () => ({
	controlCenterQueryRoot: ["project-control-center"],
	adaptiveTasksQueryRoot: ["adaptive-tasks"],
	getProjectControl: api.control,
	setProjectControl: api.setControl,
	cancelProjectWork: api.cancelWork,
	listProjectNeedsHuman: api.needsHuman,
	resolveTaskNeedsHuman: api.resolve,
	listProjectExperiments: api.experiments,
	listProjectRecommendations: api.recommendations,
}));
vi.mock("../lib/api-client", () => ({ apiErrorMessage: (error: unknown) => error instanceof Error ? error.message : String(error) }));
const navigateMock = vi.fn();
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigateMock }));

function mount() {
	const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={client}>
			<ControlCenterView projectId="proj" />
		</QueryClientProvider>,
	);
}

function controlView(overrides: Record<string, unknown> = {}) {
	return {
		view: {
			control: { actor: { id: "local-user", kind: "USER" }, projectId: "proj", reason: "Nightly maintenance", state: "running", updatedAt: "2026-09-18T22:00:00.000Z" },
			effectiveState: "running",
			activeAttempts: 2,
			...overrides,
		},
	};
}

function needsHumanItem(overrides: Record<string, unknown> = {}) {
	return {
		id: "nh-1",
		taskId: "task-4",
		projectId: "proj",
		reasonCode: "approval_required",
		detail: "Publishing to production needs sign-off",
		actor: { id: "session-2", kind: "WORKER" },
		createdAt: "2026-09-19T06:00:00.000Z",
		resolution: null,
		...overrides,
	};
}

beforeEach(() => {
	vi.clearAllMocks();
	window.confirm = vi.fn(() => true);
	api.control.mockResolvedValue(controlView());
	api.setControl.mockResolvedValue(controlView());
	api.cancelWork.mockResolvedValue({
		cancel: { killServiceWired: true, result: { cancelled: ["task-1", "task-2"], retained: ["task-3"], scope: "pending" }, terminations: [{ sessionId: "session-7", error: "" }, { sessionId: "session-8", error: "kill service unavailable" }] },
	});
	api.needsHuman.mockResolvedValue({ items: [needsHumanItem()], nextAfterId: "" });
	api.resolve.mockResolvedValue(needsHumanItem({ resolution: { resolution: "approved", actor: { id: "local-user", kind: "USER" }, resolvedAt: "2026-09-19T08:00:00.000Z" } }));
	api.experiments.mockResolvedValue({ items: [], nextAfterId: "" });
	api.recommendations.mockResolvedValue({ items: [], nextAfterId: "" });
});

describe("ControlCenterView", () => {
	it("shows the effective control state with its provenance and live attempts", async () => {
		mount();
		expect(await screen.findByText("running")).toBeInTheDocument();
		expect(screen.getByText(/2 active attempts/)).toBeInTheDocument();
		expect(screen.getByText(/Nightly maintenance/)).toBeInTheDocument();
	});

	it("offers only the transitions the stored state machine accepts", async () => {
		api.control.mockResolvedValue(controlView());
		mount();
		expect(await screen.findByText("running")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Pause" })).toBeEnabled();
		expect(screen.getByRole("button", { name: "Drain" })).toBeEnabled();
		expect(screen.getByRole("button", { name: "Stop" })).toBeEnabled();
		expect(screen.getByRole("button", { name: "Resume" })).toBeDisabled();
	});

	it("disables every invalid target once stopped", async () => {
		api.control.mockResolvedValue(controlView({ control: { actor: { id: "local-user", kind: "USER" }, projectId: "proj", reason: "", state: "stopped", updatedAt: "2026-09-18T22:00:00.000Z" }, effectiveState: "stopped", activeAttempts: 0 }));
		mount();
		expect(await screen.findByText("stopped")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Resume" })).toBeEnabled();
		for (const label of ["Pause", "Drain", "Stop"]) {
			expect(screen.getByRole("button", { name: label })).toBeDisabled();
		}
	});

	it("pauses through the deterministic control endpoint with the recorded reason", async () => {
		mount();
		expect(await screen.findByText("running")).toBeInTheDocument();
		await userEvent.type(screen.getAllByLabelText(/Reason \(optional\)/)[0], "Hold releases");
		await userEvent.click(screen.getByRole("button", { name: "Pause" }));
		await waitFor(() => expect(api.setControl).toHaveBeenCalledWith("proj", { state: "paused", reason: "Hold releases" }));
	});

	it("requires confirmation before stopping or cancelling all work", async () => {
		window.confirm = vi.fn(() => false);
		mount();
		expect(await screen.findByText("running")).toBeInTheDocument();
		await userEvent.click(screen.getByRole("button", { name: "Stop" }));
		expect(api.setControl).not.toHaveBeenCalled();
		await userEvent.selectOptions(screen.getByLabelText("Scope"), "all");
		await userEvent.click(screen.getByRole("button", { name: "Cancel work" }));
		expect(api.cancelWork).not.toHaveBeenCalled();
	});

	it("reports bulk cancellation honestly including retained leases and failed terminations", async () => {
		mount();
		expect(await screen.findByText("running")).toBeInTheDocument();
		await userEvent.click(screen.getByRole("button", { name: "Cancel work" }));
		expect(await screen.findByText(/Cancelled 2 tasks/i)).toBeInTheDocument();
		expect(screen.getByText(/1 live leases were retained and reported/i)).toBeInTheDocument();
		expect(screen.getByText(/session-8 — kill service unavailable/)).toBeInTheDocument();
		expect(screen.getByText(/session-7 — stop requested/)).toBeInTheDocument();
	});

	it("lists Needs Human requests and resolves through the task endpoint", async () => {
		mount();
		expect(await screen.findByText(/Publishing to production needs sign-off/)).toBeInTheDocument();
		expect(screen.getByText("approval_required")).toBeInTheDocument();
		await userEvent.type(screen.getByLabelText("Resolution"), "Approved for tonight");
		await userEvent.click(screen.getByRole("button", { name: "Resolve" }));
		await waitFor(() => expect(api.resolve).toHaveBeenCalledWith("task-4", { resolution: "Approved for tonight" }));
	});

	it("renders sealed experiments and recommendations with their policy status", async () => {
		api.experiments.mockResolvedValue({
			items: [{
				id: "exp-1", projectId: "proj", kind: "agent_type", entryId: "type-a", controlVersion: 2, candidateVersion: 3,
				hypothesis: "Stricter review prompts reduce retries", minimumSamples: 20, status: "running", createdAt: "2026-09-17T08:00:00.000Z", conclusion: null,
			}],
			nextAfterId: "",
		});
		api.recommendations.mockResolvedValue({
			items: [{
				id: "rec-1", projectId: "proj", kind: "skill", entryId: "skill-b", fromVersion: 1, observation: "Pin the repo layout in the prompt",
				sampleSize: 34, status: "pending", createdAt: "2026-09-18T08:00:00.000Z",
				proposed: { skill: { capabilities: [], instructions: "Pin the repository layout at the top.", requiredMcpServers: [], requiredTools: [], resources: [] } },
				decision: null,
			}],
			nextAfterId: "",
		});
		mount();
		expect(await screen.findByText(/Stricter review prompts reduce retries/i)).toBeInTheDocument();
		expect(screen.getByText(/control v2 vs candidate v3 · minimum 20 samples/i)).toBeInTheDocument();
		expect(await screen.findByText(/Pin the repo layout in the prompt/i)).toBeInTheDocument();
		expect(screen.getByText(/from v1 · 34 samples/i)).toBeInTheDocument();
	});

	it("recovers from a control read failure", async () => {
		api.control.mockRejectedValueOnce(new Error("control store busy"));
		mount();
		expect(await screen.findByRole("alert")).toHaveTextContent(/control store busy/);
		api.control.mockResolvedValue(controlView());
		await userEvent.click(screen.getByRole("button", { name: "Retry" }));
		expect(await screen.findByText("running")).toBeInTheDocument();
	});

	it("reports an empty Needs Human queue honestly", async () => {
		api.needsHuman.mockResolvedValue({ items: [], nextAfterId: "" });
		mount();
		expect(await screen.findByText(/No open requests for human input/i)).toBeInTheDocument();
	});
});
