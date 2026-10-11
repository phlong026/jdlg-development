// Add keyboard behavior only to the existing dialogs in the reviewed admin pages.
// Closing always goes through the original close button and its business guards.
const managedPages = new Set([
    '/dashboard', '/users', '/user-workbench', '/designer-center', '/certification',
    '/membership-operations', '/recharge', '/virtualpay', '/pricing-config',
    '/recharge-config', '/membership-plans', '/ai-overview', '/ai-tasks',
    '/ai-content-audits', '/ai-tools', '/ai-pricing', '/ai-config',
]);

const panelSelector = '.modal-content, .cert-drawer, .order-drawer, .aip-modal, .aca-modal, .aca-preview';
const closeSelector = '.modal-close, .cert-drawer-close, .drawer-close, .aip-modal-close, .aca-modal-close';
const focusSelector = 'a[href], button, input, select, textarea, [tabindex], [contenteditable="true"]';

type SavedAttribute = { element: HTMLElement; name: string; value: string | null };
type DialogState = { returnFocus: HTMLElement | null; attributes: SavedAttribute[] };

export const isManagedDialogPage = (path: string) => managedPages.has(path);

export function installAdminDialogKeyboard(root: HTMLElement): () => void {
    if (!isManagedDialogPage(root.dataset.adminPage || '')) return () => {};
    const doc = root.ownerDocument;
    const view = doc.defaultView!;
    const states = new Map<HTMLElement, DialogState>();
    let lastFocused = doc.activeElement as HTMLElement | null;
    let top: HTMLElement | null = null;
    let paletteOpen = false;
    let paletteReturn: HTMLElement | null = null;
    let syncing = false;

    const available = (element: HTMLElement | null): element is HTMLElement => !!element
        && element.isConnected && !element.closest('[hidden], [inert], [aria-hidden="true"]')
        && !element.matches(':disabled') && element.getClientRects().length > 0
        && view.getComputedStyle(element).visibility !== 'hidden';
    const focusable = (panel: HTMLElement) => Array.from(panel.querySelectorAll<HTMLElement>(focusSelector))
        .filter(element => available(element) && element.tabIndex >= 0 && element.getAttribute('aria-disabled') !== 'true');
    const focus = (element: HTMLElement) => element.focus({ preventScroll: true });
    const closeButton = (panel: HTMLElement) => panel.querySelector<HTMLElement>(closeSelector)
        || (panel.matches('.aca-preview') ? panel.querySelector<HTMLElement>('button.aca-btn') : null);
    const focusPanel = (panel: HTMLElement) => {
        const active = doc.activeElement as HTMLElement | null;
        if (active && panel.contains(active) && available(active)) return;
        const close = closeButton(panel);
        const input = focusable(panel).find(element => element.matches('input:not([type="hidden"]), textarea, select'));
        // Never initially focus a potentially destructive confirmation button.
        focus(available(close) ? close : input || panel);
    };
    const paletteVisible = () => available(doc.querySelector<HTMLElement>('.cmdk-panel'));
    const level = (panel: HTMLElement) => {
        let z = 0;
        for (let element: HTMLElement | null = panel; element && element !== root; element = element.parentElement) {
            z = Math.max(z, Number(view.getComputedStyle(element).zIndex) || 0);
        }
        return z;
    };
    const setAttribute = (state: DialogState, element: HTMLElement, name: string, value: string) => {
        if (!state.attributes.some(saved => saved.element === element && saved.name === name)) {
            state.attributes.push({ element, name, value: element.getAttribute(name) });
        }
        if (element.getAttribute(name) !== value) element.setAttribute(name, value);
    };
    const restoreAttributes = (state: DialogState) => state.attributes.forEach(({ element, name, value }) => {
        if (value === null) element.removeAttribute(name);
        else element.setAttribute(name, value);
    });
    const returnTarget = (panel: HTMLElement) => {
        let target = states.get(panel)?.returnFocus || null;
        const visited = new Set<HTMLElement>([panel]);
        // Several layers can disappear together after a successful action or navigation.
        while (target && !available(target)) {
            const owner = Array.from(states.keys()).find(candidate => !visited.has(candidate) && candidate.contains(target));
            if (!owner) return null;
            visited.add(owner);
            target = states.get(owner)?.returnFocus || null;
        }
        return target;
    };
    const sync = () => {
        if (syncing) return;
        syncing = true;
        try {
            const panels = Array.from(root.querySelectorAll<HTMLElement>(panelSelector)).filter(available);
            // Stable sorting preserves DOM paint order for equal-z-index modals.
            panels.sort((left, right) => level(left) - level(right));
            for (const panel of panels) {
                if (states.has(panel)) continue;
                const state: DialogState = { returnFocus: panel.contains(lastFocused) ? null : lastFocused, attributes: [] };
                states.set(panel, state);
                if (!panel.hasAttribute('role')) setAttribute(state, panel, 'role', 'dialog');
                if (!panel.hasAttribute('tabindex')) setAttribute(state, panel, 'tabindex', '-1');
                if (!panel.hasAttribute('aria-label') && !panel.hasAttribute('aria-labelledby')) {
                    const title = panel.querySelector('h1, h2, h3')?.textContent?.trim();
                    setAttribute(state, panel, 'aria-label', title || (panel.matches('.aca-preview') ? '生成结果预览' : '操作窗口'));
                }
                const close = closeButton(panel);
                if (close && !close.hasAttribute('aria-label') && !close.hasAttribute('aria-labelledby')) {
                    setAttribute(state, close, 'aria-label', '关闭窗口');
                }
            }
            const previous = top;
            top = panels[panels.length - 1] || null;
            const hasPalette = paletteVisible();
            for (const panel of panels) setAttribute(states.get(panel)!, panel, 'aria-modal', String(panel === top && !hasPalette));
            if (hasPalette) {
                if (!paletteOpen) paletteReturn = lastFocused;
                paletteOpen = true;
                // The palette owns Escape and initial focus. Do not move focus or
                // consume keys while it is above these dialogs (z-index: 1500).
            } else {
                const restore = paletteOpen ? paletteReturn : previous && previous !== top ? returnTarget(previous) : null;
                paletteOpen = false;
                paletteReturn = null;
                if (available(restore) && (!top || top.contains(restore))) focus(restore);
                if (top) focusPanel(top);
            }
            for (const [panel, state] of states) {
                if (!panels.includes(panel)) { restoreAttributes(state); states.delete(panel); }
            }
        } finally { syncing = false; }
    };
    const onFocus = () => {
        // React autoFocus may run before the observer: save the opening control,
        // rather than the new input, by discovering the new layer first.
        if (!syncing) sync();
        lastFocused = doc.activeElement as HTMLElement | null;
    };
    const onKey = (event: KeyboardEvent) => {
        if (event.key !== 'Escape' && event.key !== 'Tab') return;
        sync();
        if (!top || paletteVisible() || event.defaultPrevented || event.isComposing) return;
        if (event.key === 'Escape') {
            event.preventDefault();
            event.stopImmediatePropagation();
            // Disabled close must never fall through to the layer underneath.
            const close = closeButton(top);
            if (available(close) && close.getAttribute('aria-disabled') !== 'true') close.click();
            return;
        }
        const items = focusable(top);
        const first = items[0], last = items[items.length - 1];
        const active = doc.activeElement;
        if (!first) { event.preventDefault(); focus(top); }
        else if (event.shiftKey && (active === first || active === top || !top.contains(active))) {
            event.preventDefault(); focus(last);
        } else if (!event.shiftKey && (active === last || active === top || !top.contains(active))) {
            event.preventDefault(); focus(first);
        }
        // A mobile sidebar must not also trap a key belonging to this modal.
        event.stopPropagation();
    };
    const observer = new MutationObserver(sync);
    observer.observe(root.closest('.layout-container') || root, {
        childList: true, subtree: true, attributes: true, attributeFilter: ['disabled', 'hidden', 'class', 'style', 'inert'],
    });
    doc.addEventListener('focusin', onFocus);
    // Bubble after controls/React handlers, before the sidebar's window handler.
    doc.addEventListener('keydown', onKey);
    sync();
    return () => {
        observer.disconnect();
        doc.removeEventListener('focusin', onFocus);
        doc.removeEventListener('keydown', onKey);
        states.forEach(restoreAttributes);
        // Route cleanup must not focus an element on the previous page.
    };
}
