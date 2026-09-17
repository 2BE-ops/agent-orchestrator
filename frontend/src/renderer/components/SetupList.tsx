import type { ReactNode } from "react";
import { cn } from "../lib/utils";

/** A set of setup choices. Rows share one container with dividers and carry no
 *  surface of their own: DESIGN.md prefers a shared list over a field of
 *  individually rounded cards. */
export function SetupList({ children, className }: { children: ReactNode; className?: string }) {
	return <div className={cn("flex w-full flex-col divide-y divide-border/60", className)}>{children}</div>;
}

export function SetupRow({ icon, label, description, trailing, disabled, selected, onClick, ariaLabel }: {
	icon: ReactNode;
	label: string;
	description?: string;
	trailing?: ReactNode;
	disabled?: boolean;
	selected?: boolean;
	onClick?: () => void;
	ariaLabel?: string;
}) {
	return (
		<button
			type="button"
			aria-label={ariaLabel ?? label}
			aria-pressed={selected}
			disabled={disabled}
			onClick={onClick}
			className={cn(
				"flex w-full items-center gap-3.5 px-1 py-3.5 text-left transition-colors hover:bg-foreground/[0.04] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60 disabled:pointer-events-none disabled:opacity-50",
				selected && "bg-foreground/[0.03]",
			)}
		>
			<span className="grid size-6 shrink-0 place-items-center text-muted-foreground [&_svg]:size-5">{icon}</span>
			<span className="min-w-0 flex-1">
				<span className="block text-sm font-medium leading-5 text-foreground">{label}</span>
				{description ? <span className="mt-0.5 block text-caption leading-snug text-muted-foreground">{description}</span> : null}
			</span>
			{trailing ? <span className="shrink-0 text-caption text-muted-foreground">{trailing}</span> : null}
		</button>
	);
}
