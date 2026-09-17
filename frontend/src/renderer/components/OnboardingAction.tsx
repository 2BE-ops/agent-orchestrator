import * as React from "react";
import { cn } from "../lib/utils";

/**
 * The onboarding call to action, taken from the Figma specimen (node 1:10).
 *
 * The surface reads as one object lit from above: a vertical grey-to-white
 * gradient, a 1px white hairline at the edge, a tight bright highlight along
 * the top, and a wide dark falloff along the bottom. Both insets are box
 * shadows, so they paint above the fill and below the label, matching the
 * ordering of the specimen's stacked layers.
 *
 * The specimen is drawn at 84px tall, so its corner and type come from this
 * app's own control scale rather than being copied at specimen size.
 */
export const OnboardingAction = React.forwardRef<
	HTMLButtonElement,
	React.ButtonHTMLAttributes<HTMLButtonElement>
>(({ className, type = "button", ...props }, ref) => (
	<button
		ref={ref}
		type={type}
		className={cn(
			"inline-flex h-10 w-auto items-center justify-center whitespace-nowrap rounded-md border border-white px-4 text-sm font-medium text-[#010101]",
			"bg-[linear-gradient(180deg,#CACACA_0%,#FDFDFD_100%)]",
			"shadow-[inset_0_3px_4.1px_rgba(255,255,255,0.8),inset_0_-4px_16px_rgba(0,0,0,0.25)]",
			"transition-[filter] duration-[100ms] ease-out hover:brightness-[1.04] active:brightness-[0.97]",
			"focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
			"disabled:cursor-not-allowed disabled:opacity-50",
			className,
		)}
		{...props}
	/>
));

OnboardingAction.displayName = "OnboardingAction";
