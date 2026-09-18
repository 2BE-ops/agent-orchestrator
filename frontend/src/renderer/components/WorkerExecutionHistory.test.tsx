import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	render,
	screen,
	waitFor,
	within,
	fireEvent,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { WorkerExecutionHistory } from "./WorkerExecutionHistory";

const { list, read } = vi.hoisted(() => ({ list: vi.fn(), read: vi.fn() }));
vi.mock("../lib/registry-api", () => ({
	listWorkerExecutions: list,
	getWorkerExecution: read,
	workerExecutionsQueryRoot: ["worker-executions"],
}));
const current = {
	effective: {
		harness: "codex",
		sessionMode: "chat",
		config: { model: "retained-model", effort: "high", permissions: "auto" },
	},
};
const event = (sequence: number, action = "applied") => ({
	activation: {
		sequence,
		operationId: `operation-${sequence}`,
		action,
		createdAt: "2026-09-18T00:00:00Z",
	},
	actor: { origin: "USER", id: "local-user" },
	reason: `Reason ${sequence}`,
	harness: "codex",
	sessionMode: "chat",
	config: current.effective.config,
});
function mount() {
	const client = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	render(
		<QueryClientProvider client={client}>
			<WorkerExecutionHistory sessionId="worker-1" />
		</QueryClientProvider>,
	);
	return client;
}
beforeEach(() => {
	list.mockReset();
	read.mockReset();
});
it("keeps unresolved native changes visible alongside the last committed configuration", async () => {
	list.mockResolvedValue({
		current,
		currentSequence: 0,
		events: [],
		pendingChange: { id: "pending-native" },
	});
	mount();
	expect(await screen.findByRole("alert")).toHaveTextContent(
		"A native configuration change needs recovery",
	);
	expect(screen.getByLabelText("Active configuration")).toHaveTextContent(
		"retained-model",
	);
});
it("paginates immutable changes and distinguishes a rollback from its source operation", async () => {
	list
		.mockResolvedValueOnce({
			current,
			currentSequence: 2,
			events: [event(1)],
			nextCursor: 1,
		})
		.mockResolvedValueOnce({
			current,
			currentSequence: 2,
			events: [event(2, "rolled_back")],
		});
	read.mockResolvedValue({
		reason: "Original attempted change",
		configuration: { systemPrompt: "Exact retained instructions" },
	});
	mount();
	expect(
		await screen.findByLabelText("Active configuration"),
	).toHaveTextContent("retained-model · high · auto");
	expect(read).not.toHaveBeenCalled();
	await userEvent.click(
		screen.getByRole("button", { name: "Load more changes" }),
	);
	expect(
		await screen.findByText("Configuration restored · Current"),
	).toBeInTheDocument();
	expect(list).toHaveBeenLastCalledWith("worker-1", 1);
	const item = screen.getAllByRole("listitem")[1];
	expect(item).toHaveTextContent("USER · local-user");
	const details = within(item)
		.getByText("Change details and retained content")
		.closest("details")!;
	details.open = true;
	fireEvent(details, new Event("toggle"));
	expect(
		await within(item).findByText(/Exact retained instructions/),
	).toBeInTheDocument();
	expect(read).toHaveBeenCalledWith("worker-1", "operation-2");
});
it("shows an explicit empty state and refreshes after a configuration event", async () => {
	list
		.mockResolvedValueOnce({ current, currentSequence: 0, events: [] })
		.mockResolvedValue({ current, currentSequence: 1, events: [event(1)] });
	const client = mount();
	expect(
		await screen.findByText("No configuration changes since launch."),
	).toBeInTheDocument();
	await client.invalidateQueries({
		queryKey: ["worker-executions", "worker-1"],
	});
	expect(await screen.findByText("Reason 1")).toBeInTheDocument();
	expect(
		screen.queryByText("No configuration changes since launch."),
	).not.toBeInTheDocument();
});
it("keeps read failures retryable without inventing an empty history", async () => {
	list
		.mockRejectedValueOnce(new Error("History unavailable"))
		.mockResolvedValueOnce({ current, currentSequence: 0, events: [] });
	mount();
	expect(await screen.findByRole("alert")).toHaveTextContent(
		"History unavailable",
	);
	expect(
		screen.queryByText("No configuration changes since launch."),
	).not.toBeInTheDocument();
	await userEvent.click(screen.getByRole("button", { name: "Retry" }));
	await waitFor(() =>
		expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
	);
});
