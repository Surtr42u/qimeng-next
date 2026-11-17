import type { ComponentProps } from "react";
import { Switch as Primitive } from "radix-ui";

export function Switch({
  className,
  ...props
}: ComponentProps<typeof Primitive.Root>) {
  return (
    <Primitive.Root
      className={["settings-switch-track", className].filter(Boolean).join(" ")}
      {...props}
    >
      <Primitive.Thumb className="settings-switch-thumb" />
    </Primitive.Root>
  );
}
