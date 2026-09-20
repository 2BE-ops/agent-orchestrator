import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { WorkerConfigurationInspector } from "./WorkerConfigurationInspector";

const read = vi.hoisted(() => vi.fn());
vi.mock("../lib/registry-api", () => ({ getWorkerConfiguration: read }));
vi.mock("./WorkerExecutionHistory", () => ({
	WorkerExecutionHistory: () => null,
}));
function mount() {
	return render(
		<QueryClientProvider
			client={
				new QueryClient({ defaultOptions: { queries: { retry: false } } })
			}
		>
			<WorkerConfigurationInspector sessionId="worker-1" />
		</QueryClientProvider>,
	);
}
beforeEach(() => read.mockReset());
it("shows retained versions and instructions independently of current registry state", async () => {
	read.mockResolvedValue({
		agentType: { name: "Old reviewer", version: 2 },
		effective: {
			harness: "codex",
			sessionMode: "tui",
			config: { model: "retained-model" },
		},
		skills: [
			{ reference: { id: "a", name: "Evidence", version: 1 } },
			{ reference: { id: "b", name: "Interfaces", version: 3 } },
		],
		origin: "USER",
		createdAt: "2026-09-18T00:00:00Z",
		selection: { overrides: { instructions: "Frozen instructions" } },
	});
	mount();
	const history = await screen.findByRole("region", {
		name: "Launch configuration",
	});
	expect(history).toHaveTextContent("Old reviewer · v2");
	expect(
		screen.getAllByRole("listitem").map((item) => item.textContent),
	).toEqual(["Evidence · v1", "Interfaces · v3"]);
	expect(history).toHaveTextContent("Frozen instructions");
	expect(read).toHaveBeenCalledWith("worker-1");
});
it("does not invent launch facts for a legacy session", async () => {
	read.mockResolvedValue(null);
	const { container } = mount();
	await waitFor(() =>
		expect(screen.queryByRole("status")).not.toBeInTheDocument(),
	);
	expect(container).toBeEmptyDOMElement();
});
it("reports unavailable history and allows retry", async () => {
	read
		.mockRejectedValueOnce(new Error("History unavailable"))
		.mockResolvedValueOnce(null);
	mount();
	expect(await screen.findByRole("alert")).toHaveTextContent(
		"History unavailable",
	);
	await userEvent.click(screen.getByRole("button", { name: "Retry" }));
	await waitFor(() =>
		expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
	);
	expect(read).toHaveBeenCalledTimes(2);
});
