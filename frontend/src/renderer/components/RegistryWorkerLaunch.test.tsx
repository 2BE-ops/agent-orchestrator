import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import type { TaskComposerProps } from "./TaskComposer";
import { RegistryWorkerLaunch } from "./RegistryWorkerLaunch";

const state = vi.hoisted(() => ({
	navigate: vi.fn(),
	closed: vi.fn(),
	props: undefined as TaskComposerProps | undefined,
}));
vi.mock("@tanstack/react-router", () => ({
	useNavigate: () => state.navigate,
}));
vi.mock("../lib/registry-api", () => ({
	registryProjectsQuery: {
		queryKey: ["registry-projects"],
		queryFn: async () => [{ id: "project", name: "Demo project" }],
	},
}));
vi.mock("./TaskComposer", () => ({
	TaskComposer: (props: TaskComposerProps) => {
		state.props = props;
		return (
			<button onClick={() => props.onCreated("worker-1")}>Submit worker</button>
		);
	},
}));
beforeEach(() => {
	vi.clearAllMocks();
	state.props = undefined;
});
function mount() {
	return render(
		<QueryClientProvider client={new QueryClient()}>
			<RegistryWorkerLaunch id="reviewer" version={4} onClose={state.closed} />
		</QueryClientProvider>,
	);
}
it("launches the selected version in the standalone workspace and opens its session", async () => {
	mount();
	expect(state.props?.initialWorkerSelection).toEqual({
		agentTypeId: "reviewer",
		version: 4,
		overrides: {},
	});
	expect(state.props?.projectId).toBe("__standalone__");
	await userEvent.click(screen.getByRole("button", { name: "Submit worker" }));
	expect(state.navigate).toHaveBeenCalledWith({
		to: "/sessions/$sessionId",
		params: { sessionId: "worker-1" },
	});
	expect(state.closed).toHaveBeenCalledOnce();
});
it("uses the chosen project for the normal composer and session navigation", async () => {
	mount();
	await screen.findByRole("option", { name: "Demo project" });
	await userEvent.selectOptions(
		screen.getByLabelText("Worker project"),
		"project",
	);
	expect(state.props?.projectId).toBe("project");
	await userEvent.click(screen.getByRole("button", { name: "Submit worker" }));
	expect(state.navigate).toHaveBeenCalledWith({
		to: "/projects/$projectId/sessions/$sessionId",
		params: { projectId: "project", sessionId: "worker-1" },
	});
});
