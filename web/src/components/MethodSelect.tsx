import * as Select from "@radix-ui/react-select";
import { Check, ChevronDown, ChevronUp } from "lucide-react";

const methods = [
  { value: "GET", tone: "green", hint: "读取资源" },
  { value: "POST", tone: "blue", hint: "创建资源" },
  { value: "PUT", tone: "amber", hint: "完整更新" },
  { value: "PATCH", tone: "purple", hint: "局部更新" },
  { value: "DELETE", tone: "red", hint: "删除资源" },
];

export function MethodSelect({value,onValueChange}:{value:string;onValueChange:(value:string)=>void}) {
  const selected=methods.find(item=>item.value===value)??methods[0];
  return <Select.Root value={value} onValueChange={onValueChange}>
    <Select.Trigger className="methodSelectTrigger" aria-label="请求方法">
      <span className={`methodSelectValue ${selected.tone}`}>{selected.value}</span>
      <Select.Icon className="methodSelectChevron"><ChevronDown size={16}/></Select.Icon>
    </Select.Trigger>
    <Select.Portal>
      <Select.Content className="methodSelectContent" position="popper" sideOffset={7} align="start">
        <Select.ScrollUpButton className="methodSelectScroll"><ChevronUp size={15}/></Select.ScrollUpButton>
        <Select.Viewport className="methodSelectViewport">
          <Select.Group>
            <Select.Label className="methodSelectLabel">HTTP METHOD</Select.Label>
            {methods.map(item=><Select.Item key={item.value} value={item.value} className="methodSelectItem">
              <Select.ItemIndicator className="methodSelectIndicator"><Check size={14}/></Select.ItemIndicator>
              <Select.ItemText><span className={`methodSelectBadge ${item.tone}`}>{item.value}</span></Select.ItemText>
              <span className="methodSelectHint">{item.hint}</span>
            </Select.Item>)}
          </Select.Group>
        </Select.Viewport>
        <Select.ScrollDownButton className="methodSelectScroll"><ChevronDown size={15}/></Select.ScrollDownButton>
      </Select.Content>
    </Select.Portal>
  </Select.Root>
}
