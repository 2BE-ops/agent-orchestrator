import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	ArrowUpRight,
	Bell,
	FileText,
	GitMerge,
	Globe2,
	LayoutDashboard,
	LayoutList,
	MessageSquare,
	PanelRight,
	Plus,
	TriangleAlert,
} from "lucide-react";
import {
	InspectorActivityTimelineView,
	InspectorSection,
	SessionInspectorShellView,
	type InspectorTab,
	type InspectorTimelineEvent,
} from "@aoagents/product-ui";
import { cn } from "../lib/utils";
import { StatusPill } from "./StatusPill";
import { TopbarButton } from "./TopbarButton";
import { Button } from "./ui/button";
import { Switch } from "./ui/switch";

/** The frame is drawn at the app's real desktop size and then scaled down as a
 *  whole. That is the difference between this and a hand-built mock: the
 *  chrome, buttons, radii and type keep their real proportions, and only the
 *  final transform changes. Hand-tuned preview sizes shrink the type but leave
 *  1px borders and radii at full size, which is what makes a mock read as
 *  "almost the app". */
const FRAME_WIDTH = 1180;
const FRAME_HEIGHT = 720;

const PHASES = ["working", "checks", "review"] as const;
type Phase = (typeof PHASES)[number];

const PHASE_MS = 4200;

const TONE = {
	working: "var(--color-status-working)",
	success: "var(--color-status-ready)",
	error: "var(--color-status-exited)",
	passive: "var(--color-passive)",
} as const;

export function OnboardingFeedbackPreview() {
	const containerRef = useRef<HTMLDivElement>(null);
	const [scale, setScale] = useState(1);
	const [phaseIndex, setPhaseIndex] = useState(0);

	useEffect(() => {
		const element = containerRef.current;
		if (!element) return;
		const measure = () => {
			const { width, height } = element.getBoundingClientRect();
			if (!width || !height) return;
			setScale(Math.min(width / FRAME_WIDTH, height / FRAME_HEIGHT));
		};
		measure();
		const observer = new ResizeObserver(measure);
		observer.observe(element);
		return () => observer.disconnect();
	}, []);

	useEffect(() => {
		const timer = window.setInterval(() => {
			setPhaseIndex((index) => (index + 1) % PHASES.length);
		}, PHASE_MS);
		return () => window.clearInterval(timer);
	}, []);

	const phase = PHASES[phaseIndex];

	return (
		<div ref={containerRef} className="size-full">
			<div
				aria-hidden="true"
				className="pointer-events-none origin-top-left select-none overflow-hidden rounded-lg border border-border-strong bg-background shadow-xl"
				style={{
					height: FRAME_HEIGHT,
					transform: `scale(${scale})`,
					width: FRAME_WIDTH,
				}}
			>
				<SessionFrame phase={phase} />
			</div>
		</div>
	);
}

function SessionFrame({ phase }: { phase: Phase }) {
	return (
		<div className="flex size-full flex-col bg-background text-foreground">
			<PreviewTopbar phase={phase} />
			<div className="flex min-h-0 flex-1">
				<TerminalSurface phase={phase} />
				<PreviewInspector phase={phase} />
			</div>
		</div>
	);
}

/** Mirrors ShellTopbar: project identity on the left, the app's own
 *  TopbarButton controls on the right. */
function PreviewTopbar({ phase }: { phase: Phase }) {
	const { t } = useTranslation();
	return (
		<div className="flex h-toolbar shrink-0 items-center gap-3 border-b border-border-strong px-3">
			<div className="flex min-w-0 items-center gap-2.5">
				<span className="truncate text-brand font-semibold leading-none tracking-tight text-foreground">
					ao-onboarding
				</span>
				<span aria-hidden="true" className="h-4 w-px shrink-0 bg-foreground/25" />
				<span className="truncate text-sm font-semibold leading-none text-foreground">github-auth</span>
				<StatusPill
					breathe={phase === "working"}
					className="px-2 py-1 text-micro"
					label={phase === "working" ? t("status.working") : t("displayStatus.needsReview")}
					leading="none"
					tone={phase === "working" ? TONE.working : TONE.success}
				/>
			</div>
			<div className="flex min-w-0 flex-1" />
			<div className="flex shrink-0 items-center gap-1">
				<TopbarButton className="topbar-control--labeled" variant="accent">
					<Plus className="size-icon-md" aria-hidden="true" />
					{t("newTask.task")}
				</TopbarButton>
				<TopbarButton className="topbar-control--labeled" variant="feature">
					<LayoutDashboard className="size-icon-md" aria-hidden="true" />
					{t("shell.openKanban")}
				</TopbarButton>
				<TopbarButton aria-label={t("shortcut.toggle-inspector")} variant="icon">
					<PanelRight className="size-icon-md" aria-hidden="true" />
				</TopbarButton>
				<TopbarButton aria-label={t("notify.title")} variant="icon">
					<Bell className="size-icon-md" aria-hidden="true" />
				</TopbarButton>
			</div>
		</div>
	);
}

const TERMINAL_LINES: { marker?: string; text: string; tone?: "error" | "success" }[] = [
	{ marker: ">", text: "Add GitHub OAuth callback handling." },
	{ marker: "*", text: "Write(src/auth/callback.ts)" },
	{ marker: "*", text: "Bash(npm test -- auth)" },
	{ marker: "L", text: "Tests 11 passed (11)" },
	{ marker: "*", text: "Bash(git push && gh pr create --fill)" },
	{ marker: "L", text: "Created pull request #2481" },
	{ text: "" },
	{ text: "CI is failing on your PR.", tone: "error" },
	{ text: "Failed: test / web (failed)", tone: "error" },
	{ text: "FAIL src/auth/callback.test.ts", tone: "error" },
	{ text: "  expected 401, received 500", tone: "error" },
	{ text: "Fix the issues and push again." },
];

/** Terminal surface, painted with the app's terminal tokens rather than a
 *  preview-only palette. */
function TerminalSurface({ phase }: { phase: Phase }) {
	return (
		<div className="flex min-w-0 flex-1 flex-col justify-end overflow-hidden bg-[var(--color-terminal-opaque)] px-4 py-3.5 font-mono text-[13px] leading-6 text-[var(--color-text-terminal)]">
			<div className="mb-3 flex items-start gap-2.5">
				<img alt="" aria-hidden="true" className="mt-0.5 size-5 shrink-0" src="/app-icons/agents/claude-code.svg" />
				<div className="min-w-0">
					<div>
						<span className="font-bold text-[var(--color-text-terminal)]">Claude Code</span>
						<span className="text-terminal-dim"> v2.1.204</span>
					</div>
					<div className="text-terminal-dim">Opus 4.8 (1M context) · Claude Team</div>
					<div className="text-terminal-dim">~/ao/repos/orchestrator</div>
				</div>
			</div>
			{TERMINAL_LINES.map((line, index) => (
				<div className="flex gap-2" key={index}>
					{line.marker ? <span className="w-3 shrink-0 text-terminal-dim">{line.marker}</span> : <span className="w-3 shrink-0" />}
					<span className={cn(line.tone === "error" && "text-error", line.tone === "success" && "text-success")}>
						{line.text}
						{index === TERMINAL_LINES.length - 1 && phase !== "working" ? (
							<span className="ml-1 inline-block h-3.5 w-1.5 animate-status-pulse bg-[var(--color-text-terminal)] align-middle" />
						) : null}
					</span>
				</div>
			))}
		</div>
	);
}

function PreviewInspector({ phase }: { phase: Phase }) {
	const { t } = useTranslation();

	const tabs: InspectorTab[] = [
		{ icon: <LayoutList aria-hidden="true" />, id: "summary", label: t("inspector.summary") },
		{ icon: <MessageSquare aria-hidden="true" />, id: "reviews", label: t("inspector.reviewTab") },
		{ icon: <Globe2 aria-hidden="true" />, id: "browser", label: t("inspector.browser") },
		{ icon: <FileText aria-hidden="true" />, id: "files", label: t("inspector.files") },
	];

	const events: InspectorTimelineEvent[] = [
		{
			content: (
				<span className="flex items-center gap-2 text-xs">
					<StatusPill
						breathe={phase === "working"}
						className="px-2 py-0.5 text-2xs"
						label={phase === "working" ? t("status.working") : t("displayStatus.needsReview")}
						leading="none"
						tone={phase === "working" ? TONE.working : TONE.success}
					/>
					{phase === "checks" ? (
						<StatusPill
							breathe
							className="px-2 py-0.5 text-2xs"
							label={t("pr.card.checksFailing")}
							leading="none"
							tone={TONE.error}
						/>
					) : null}
				</span>
			),
			markerBreathe: phase === "working",
			markerTone: phase === "working" ? TONE.working : TONE.success,
			timestamp: null,
			tone: phase === "working" ? "now" : "good",
		},
		{
			content: (
				<span className="text-xs text-muted-foreground">
					Opened <span className="font-mono text-settings-label">PR #2481</span>
				</span>
			),
			timestamp: "18m ago",
			tone: "neutral",
		},
	];

	return (
		<div className="w-[360px] shrink-0 border-l border-border-strong">
			<SessionInspectorShellView
				activeView="summary"
				ariaLabel={t("inspector.summary")}
				browserPoppedOut={false}
				onViewChange={() => undefined}
				tabs={tabs}
				summaryView={
					<div className="p-3 pb-4">
						<InspectorSection title={t("inspector.pullRequest")}>
							<PullRequestCard phase={phase} />
						</InspectorSection>
						<InspectorSection
							action={<Button size="sm" variant="secondary">{t("inspector.review.run")}</Button>}
							title={t("inspector.reviews")}
						>
							<div className="flex items-center gap-2 py-1.5">
								<span className="size-1.5 shrink-0 rounded-full bg-passive" />
								<span className="truncate text-xs text-settings-muted">{t("inspector.review.notRun")}</span>
							</div>
						</InspectorSection>
						<InspectorSection title={t("inspector.completion")}>
							<div className="flex items-center justify-between gap-3 py-1.5">
								<span className="truncate text-xs text-settings-label">{t("inspector.terminateOnMergeShort")}</span>
								<Switch aria-label={t("inspector.terminateOnMergeShort")} defaultChecked />
							</div>
						</InspectorSection>
						<InspectorSection title={t("inspector.activity")}>
							<InspectorActivityTimelineView events={events} />
						</InspectorSection>
					</div>
				}
			/>
		</div>
	);
}

/** Uses the app's card measurement and state tones verbatim. Built here rather
 *  than through InspectorPullRequestCardView because that view takes a fully
 *  derived presentation object, and a preview should not have to fake the
 *  daemon's SCM summary to render a card. */
function PullRequestCard({ phase }: { phase: Phase }) {
	const { t } = useTranslation();
	const failing = phase === "checks";
	const merged = phase === "review";

	return (
		<article className="w-full min-w-0 rounded-lg border border-(--color-border-settings-input) bg-(--color-bg-settings-input) px-3 py-2.5">
			<span className="inline text-sm font-semibold leading-snug tracking-tight text-settings-label">
				Reject expired OAuth state
			</span>
			<div className="mt-1.5 flex min-w-0 items-center gap-2">
				<span className="inline-flex min-w-0 items-center gap-1 font-mono text-xs font-medium text-settings-label">
					<GitMerge aria-hidden="true" className="size-icon-sm shrink-0" />
					<span>PR #2481</span>
					<ArrowUpRight aria-hidden="true" className="size-icon-2xs shrink-0" />
				</span>
				<span
					className={cn(
						"inline-flex h-5 shrink-0 items-center justify-center overflow-hidden whitespace-nowrap rounded-full border px-1.5 text-[9px] font-medium leading-none",
						merged ? "border-border-strong bg-overlay text-success" : "border-border-strong bg-overlay text-muted-foreground",
					)}
				>
					{merged ? t("pr.state.merged") : t("pr.state.open")}
				</span>
			</div>
			<div className="mt-1.5 font-mono text-xs text-muted-foreground">
				feat/github-auth → main
				<span className="px-1 text-passive">·</span>3 files
				<span className="px-1 text-passive">·</span>
				<span className="text-success">+41</span>
				<span className="px-1 text-passive">·</span>
				<span className="text-error">-12</span>
			</div>
			{failing ? (
				<div className="mt-2 flex items-center gap-2 rounded-md bg-error/8 px-2 py-1.5">
					<TriangleAlert aria-hidden="true" className="size-icon-sm shrink-0 text-error" />
					<span className="truncate text-xs text-error">{t("pr.card.checksFailing")}</span>
				</div>
			) : null}
		</article>
	);
}
