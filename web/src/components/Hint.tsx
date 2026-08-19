import * as Tooltip from "@radix-ui/react-tooltip";
import type { ReactNode } from "react";

export function Hint({content,children}:{content:string;children:ReactNode}) {
  return <Tooltip.Root>
    <Tooltip.Trigger asChild>{children}</Tooltip.Trigger>
    <Tooltip.Portal>
      <Tooltip.Content className="hintContent" sideOffset={8} collisionPadding={10}>
        {content}<Tooltip.Arrow className="hintArrow"/>
      </Tooltip.Content>
    </Tooltip.Portal>
  </Tooltip.Root>
}

export { Tooltip };
