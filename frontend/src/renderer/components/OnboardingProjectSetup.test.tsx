import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";

const bridgeMocks = vi.hoisted(() => ({ chooseDirectory: vi.fn(), getRepositoryBranch: vi.fn() }));
const apiMocks = vi.hoisted(() => ({ POST: vi.fn() }));

vi.mock("../lib/bridge", () => ({
	aoBridge: { app: { chooseDirectory: bridgeMocks.chooseDirectory, getRepositoryBranch: bridgeMocks.getRepositoryBranch } },
}));

vi.mock("../lib/api-client", () => ({
	apiClient: { POST: apiMocks.POST },
	apiErrorMessage: (_error: unknown, fallback: string) => fallback,
}));

vi.mock("./CreateProjectFlow", () => ({
	CreateProjectFlow: () => null,
}));

import { OnboardingProjectSetup } from "./OnboardingProjectSetup";

beforeEach(() => {
	bridgeMocks.chooseDirectory.mockReset();
	bridgeMocks.getRepositoryBranch.mockReset().mockResolvedValue("main");
	apiMocks.POST.mockReset().mockResolvedValue({
		data: {
			isValid: true,
			nextStep: "continue",
			root: { isRepo: true, hasCommit: true },
		},
	});
});

it("advances with the folder returned by the native picker", async () => {
	bridgeMocks.chooseDirectory.mockResolvedValue("/repo/project");
	const onPrepared = vi.fn();

	render(
		<OnboardingProjectSetup
			mode="folder"
			onModeChange={vi.fn()}
			onPrepared={onPrepared}
			preparedProject={null}
		/>,
	);

	await userEvent.click(screen.getByRole("button", { name: "Open local folder" }));

	await waitFor(() => expect(onPrepared).toHaveBeenLastCalledWith({
		path: "/repo/project",
		defaultBranch: "main",
		repositorySetup: null,
	}));
});

it("carries initialization requirements forward for a plain folder", async () => {
	bridgeMocks.chooseDirectory.mockResolvedValue("/repo/plain");
	apiMocks.POST.mockResolvedValueOnce({
		data: {
			isValid: true,
			nextStep: "prepare_git",
			root: { isRepo: false, hasCommit: false },
		},
	});
	const onPrepared = vi.fn();

	render(
		<OnboardingProjectSetup
			mode="folder"
			onModeChange={vi.fn()}
			onPrepared={onPrepared}
			preparedProject={null}
		/>,
	);

	await userEvent.click(screen.getByRole("button", { name: "Open local folder" }));

	await waitFor(() => expect(onPrepared).toHaveBeenLastCalledWith(expect.objectContaining({
		path: "/repo/plain",
		repositorySetup: "NOT_A_GIT_REPO",
	})));
});
