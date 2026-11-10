/**
 * Scrollbars that show only on activity (TASK-428). WebView2 has no overlay scrollbar, so the thumb is transparent at rest
 * (ui/scrollbar.css) and this module puts the class `scroll-active` on the scroller that is being scrolled or pointed at.
 * It removes the class `hideAfterMs` after the last activity. The scrollbar track keeps its width all the time
 * (`scrollbar-gutter: stable`), so nothing moves when the thumb shows.
 * Activity: a `scroll` event (wheel, keys, drag: it does not bubble, hence the capture phase), the pointer over the
 * scrollbar strip of the nearest scroller (checked once per frame), a press in the strip (a thumb drag) until release.
 */
export const activeClass = "scroll-active";
export const hideAfterMs = 1000;

type Frame = (run: () => void) => unknown;
type Move = { target: EventTarget | null; x: number; y: number; buttons: number };

// Duck-typed (no `instanceof Element`): the unit tests run in Node with fake elements.
function isElement(t: EventTarget | null): t is Element {
  return t !== null && typeof (t as Element).classList === "object";
}

/** `raf` is for tests; by default the one of the document's window, or none (then the pointer is handled at once). */
export function installScrollActivity(doc: Document, raf?: Frame): () => void {
  const win = doc.defaultView;
  const frame: Frame | null = raf ?? (win ? (run) => win.requestAnimationFrame(run) : null);

  const active = new Set<Element>();
  const timers = new WeakMap<Element, ReturnType<typeof setTimeout>>();
  let hovered: Element | null = null; // the pointer rests on its scrollbar: no timer while it does
  let dragged: Element | null = null; // a thumb drag is on: no timer until the release

  const stop = (el: Element) => {
    const t = timers.get(el);
    if (t !== undefined) {
      clearTimeout(t);
      timers.delete(el);
    }
  };
  const hide = (el: Element) => {
    stop(el);
    el.classList.remove(activeClass);
    active.delete(el);
  };
  // Shows the thumb and (re)starts the countdown, unless the pointer or a drag holds it.
  const touch = (el: Element) => {
    el.classList.add(activeClass);
    active.add(el);
    stop(el);
    if (el !== hovered && el !== dragged) {
      timers.set(el, setTimeout(() => hide(el), hideAfterMs));
    }
  };
  const release = (el: Element | null) => {
    if (el !== null && active.has(el)) {
      touch(el);
    }
  };
  const setHovered = (el: Element | null) => {
    const prev = hovered;
    hovered = el;
    if (el !== null) {
      touch(el);
    }
    if (prev !== null && prev !== el) {
      release(prev);
    }
  };

  const onScroll = (ev: Event) => {
    const t = ev.target;
    const el = t === doc ? (doc.scrollingElement ?? doc.documentElement) : isElement(t) ? t : null;
    if (el !== null) {
      touch(el);
    }
  };

  const strip = (target: EventTarget | null, x: number, y: number): Element | null => {
    if (!win || !isElement(target)) {
      return null;
    }
    for (let e: Element | null = target; e !== null && e !== doc.documentElement; e = e.parentElement) {
      const box = e as HTMLElement;
      // Cheap filter first: a scrollbar or a right/bottom border is what makes the border box wider than the client box.
      if (box.offsetWidth - box.clientWidth - box.clientLeft <= 0 && box.offsetHeight - box.clientHeight - box.clientTop <= 0) {
        continue;
      }
      const cs = win.getComputedStyle(box);
      const right = parseFloat(cs.borderRightWidth) || 0;
      const bottom = parseFloat(cs.borderBottomWidth) || 0;
      const vertical = box.offsetWidth - box.clientWidth - box.clientLeft - right;
      const horizontal = box.offsetHeight - box.clientHeight - box.clientTop - bottom;
      if (vertical <= 0 && horizontal <= 0) {
        continue;
      }
      // The nearest box with a scrollbar decides: a nested scroller lies inside the client box of the outer one.
      const r = box.getBoundingClientRect();
      const inV = vertical > 0 && x >= r.right - right - vertical && x <= r.right - right && y >= r.top && y <= r.bottom;
      const inH = horizontal > 0 && y >= r.bottom - bottom - horizontal && y <= r.bottom - bottom && x >= r.left && x <= r.right;
      return inV || inH ? e : null;
    }
    return null;
  };

  let pending: Move | null = null;
  let queued = false;
  const move = (m: Move) => {
    // A release outside the window is not seen: the next move without a pressed button ends the drag.
    if (dragged !== null && m.buttons === 0) {
      const d = dragged;
      dragged = null;
      release(d);
    }
    setHovered(strip(m.target, m.x, m.y));
  };
  const onMove = (ev: Event) => {
    const p = ev as PointerEvent;
    pending = { target: p.target, x: p.clientX, y: p.clientY, buttons: p.buttons };
    if (frame === null) {
      move(pending);
      pending = null;
      return;
    }
    if (!queued) {
      queued = true;
      frame(() => {
        queued = false;
        const m = pending;
        pending = null;
        if (m !== null) {
          move(m);
        }
      });
    }
  };

  const onDown = (ev: Event) => {
    const p = ev as PointerEvent;
    const el = strip(p.target, p.clientX, p.clientY);
    if (el !== null) {
      dragged = el;
      touch(el);
    }
  };
  const onUp = () => {
    const d = dragged;
    dragged = null;
    release(d);
  };
  // The pointer left the window from the scrollbar: no more moves come, so the hold ends here.
  const onOut = (ev: Event) => {
    if ((ev as PointerEvent).relatedTarget === null) {
      pending = null;
      setHovered(null);
    }
  };

  const passive = { capture: true, passive: true } as const;
  doc.addEventListener("scroll", onScroll, passive);
  doc.addEventListener("pointermove", onMove, passive);
  doc.addEventListener("pointerdown", onDown, passive);
  doc.addEventListener("pointerup", onUp, passive);
  doc.addEventListener("pointercancel", onUp, passive);
  doc.addEventListener("pointerout", onOut, passive);

  return () => {
    doc.removeEventListener("scroll", onScroll, passive);
    doc.removeEventListener("pointermove", onMove, passive);
    doc.removeEventListener("pointerdown", onDown, passive);
    doc.removeEventListener("pointerup", onUp, passive);
    doc.removeEventListener("pointercancel", onUp, passive);
    doc.removeEventListener("pointerout", onOut, passive);
    pending = null;
    for (const el of [...active]) {
      hide(el);
    }
    hovered = null;
    dragged = null;
  };
}
