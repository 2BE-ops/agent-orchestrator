import type { ReactNode } from "react";
import { cn } from "../lib/utils";

/** One setup choice in the onboarding flow. Matches the project step's source
 *  buttons exactly: a flat card surface with an icon slot, no border, and no
 *  background behind the icon. */
export function SetupActionButton({ icon, label, description, trailing, disabled, onClick, ariaLabel }: {
	icon: ReactNode;
	label: string;
	description?: string;
	trailing?: ReactNode;
	disabled?: boolean;
	onClick?: () => void;
	ariaLabel?: string;
}) {
	return (
		<button
			type="button"
			// The description is supporting copy, so it must not become part of the
			// control's accessible name.
			aria-label={ariaLabel ?? label}
			disabled={disabled}
			onClick={onClick}
			className={cn(
				"flex w-full items-center gap-3 rounded-lg bg-card px-4 py-3 text-left hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/60 active:scale-[0.99] disabled:pointer-events-none disabled:opacity-50",
			)}
		>
			<span className="grid size-8 shrink-0 place-items-center text-muted-foreground [&_svg]:size-4">{icon}</span>
			<span className="min-w-0 flex-1">
				<span className="block truncate text-sm font-medium leading-5 text-foreground">{label}</span>
				{description ? <span className="mt-0.5 block text-caption leading-snug text-muted-foreground">{description}</span> : null}
			</span>
			{trailing ? <span className="shrink-0 text-caption text-muted-foreground">{trailing}</span> : null}
		</button>
	);
}
