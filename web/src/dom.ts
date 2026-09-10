// web/src/dom.ts
//
// Minimal element helper used instead of a framework. Building nodes rather
// than assigning innerHTML means recipe text from Instagram captions is never
// parsed as HTML, so untrusted caption content cannot inject markup.

type Props = Record<string, string | ((ev: Event) => void)>;

/**
 * el creates an element, applying `props` as attributes — or, for keys
 * starting with "on", as event listeners — and appending `children`, where
 * strings become text nodes.
 *
 * Note on boolean attributes: every string value is passed straight to
 * setAttribute, and `setAttribute("disabled", "")` still disables the element,
 * because HTML keys off the attribute's presence and not its value. So
 * `{ disabled: cond ? "true" : "" }` is always disabled. Toggle those via the
 * DOM property instead — `btn.disabled = cond`.
 */
export function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  props: Props = {},
  children: (Node | string)[] = [],
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props)) {
    if (key.startsWith("on") && typeof value === "function") {
      node.addEventListener(key.slice(2).toLowerCase(), value as EventListener);
    } else if (typeof value === "string") {
      node.setAttribute(key, value);
    }
  }
  for (const child of children) {
    node.append(typeof child === "string" ? document.createTextNode(child) : child);
  }
  return node;
}

/** clear removes every child of node, for re-rendering a container in place. */
export function clear(node: Element): void {
  while (node.firstChild) node.removeChild(node.firstChild);
}
