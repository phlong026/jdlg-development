// Compiled CSS contracts, not browser or screenshot verification.
import assert from 'node:assert/strict';
import test from 'node:test';
import * as sass from 'sass';
import { fileURLToPath } from 'node:url';
import postcss from 'postcss';

const root = '.layout-content[data-admin-page="/share-tracking"] .share-tracking';
const css = postcss.parse(sass.compile(fileURLToPath(new URL('../src/pages/shareTracking.scss', import.meta.url)), { logger: sass.Logger.silent }).css);
const viewports = [[320, 568], [375, 667], [390, 844], [768, 1024], [1024, 768], [1440, 900], [1024, 400]];
function matchesMedia(rule, width, height, reducedMotion) {
    for (let parent = rule.parent; parent; parent = parent.parent) {
        if (parent.type !== 'atrule' || parent.name !== 'media') continue;
        if (!parent.params.split(',').some(query => {
            if (/prefers-reduced-motion:\s*reduce/.test(query)) return reducedMotion;
            const bounds = [...query.matchAll(/(max|min)-(width|height):\s*(\d+)px/g)];
            assert.ok(bounds.length, `Unsupported media query in contract helper: ${query}`);
            return bounds.every(([, bound, axis, limit]) => bound === 'max' ? (axis === 'width' ? width : height) <= Number(limit) : (axis === 'width' ? width : height) >= Number(limit));
        })) return false;
    }
    return true;
}
// Resolve only exact compiled selectors, in stylesheet source order. Tests use
// each relevant contextual selector explicitly rather than guessing DOM matches.
function styles(target = '', width = 1440, height = 900, reducedMotion = false) {
    const result = {};
    css.walkRules(rule => {
        if (!rule.selectors.includes(`${root}${target ? ` ${target}` : ''}`) || !matchesMedia(rule, width, height, reducedMotion)) return;
        rule.walkDecls(decl => { result[decl.prop] = decl.value; });
    });
    return result;
}

test('all tracking declarations remain inside the existing route and page root', () => {
    let count = 0;
    css.walkRules(rule => {
        for (const selector of rule.selectors) {
            count++;
            assert.ok(selector === root || selector.startsWith(`${root} `), selector);
            assert.doesNotMatch(selector, /:root|\.layout-sidebar|\.layout-topbar/);
        }
    });
    assert.ok(count > 100);
});

test('warm reference palette has readable primary, metric and supporting text contrast', () => {
    const tokens = styles();
    assert.equal(tokens['--st-accent'], '#94582f');
    assert.equal(tokens['--st-cream'], '#fbf3e9');
    assert.equal(tokens['--st-metric'], '#c83916');
    const luminance = hex => {
        const rgb = hex.slice(1).match(/../g).map(value => parseInt(value, 16) / 255).map(value => value <= .04045 ? value / 12.92 : ((value + .055) / 1.055) ** 2.4);
        return rgb[0] * .2126 + rgb[1] * .7152 + rgb[2] * .0722;
    };
    const contrast = (a, b) => { const x = luminance(a), y = luminance(b); return (Math.max(x, y) + .05) / (Math.min(x, y) + .05); };
    for (const [a, b] of [[tokens['--st-accent'], '#ffffff'], [tokens['--st-metric'], '#f8f6f3'], [tokens['--st-muted'], tokens['--st-cream']]]) assert.ok(contrast(a, b) >= 4.5, `${a} on ${b}`);
    assert.equal(styles('.st-primary:hover:not(:disabled)').color, '#fff');
    assert.equal(styles('.st-metrics dd').color, 'var(--st-metric)');
    assert.equal(styles('.st-duration-pair dd').color, 'var(--st-metric)');
});

for (const [width, height] of viewports) {
    test(`${width}×${height}: shrinkable filters, cards, and reachable controls`, () => {
        assert.equal(styles('', width, height)['min-width'], '0');
        assert.equal(styles('.st-search', width, height)['min-width'], '0');
        assert.equal(styles('.st-search', width, height)['max-width'], '100%');
        assert.equal(styles('.st-content-summary', width, height)['min-width'], '0');
        assert.equal(styles('.st-content-card', width, height)['min-width'], '0');
        assert.equal(styles('button', width, height)['min-height'], '44px');
        assert.equal(styles('button', width, height)['min-width'], '44px');
        assert.equal(styles('button', width, height)['white-space'], 'normal');
        assert.equal(styles('.st-media a', width, height)['min-height'], '44px');
        assert.equal(styles('select', width, height)['min-height'], '44px');
        assert.equal(styles('.st-definitions summary', width, height)['min-height'], '44px');
        if (width <= 768) {
            assert.equal(styles('', width, height).padding, '0');
            assert.equal(styles('select', width, height)['font-size'], '16px');
            assert.equal(styles('.st-filter-fields', width, height)['grid-template-columns'], 'repeat(2, minmax(0, 1fr))');
        }
        if (width <= 360) assert.equal(styles('.st-range', width, height)['grid-template-columns'], 'repeat(3, minmax(0, 1fr))');
    });
    test(`${width}×${height}: whole landscape/portrait source remains visible`, () => {
        for (const selector of ['.st-media img', '.st-media video', '.st-dialog .st-media img', '.st-dialog .st-media video', '.st-body img', '.st-body video']) assert.equal(styles(selector, width, height)['object-fit'], 'contain', selector);
        assert.equal(styles('.st-media img', width, height)['object-position'], 'center');
        assert.equal(styles('.st-media', width, height).overflow, undefined);
        assert.equal(styles('.st-media a', width, height)['max-width'], '100%');
    });
    test(`${width}×${height}: content list stays full width, nested visitors adapt`, () => {
        assert.equal(styles('.st-cards:has(> .st-content-card)', width, height)['grid-template-columns'], 'minmax(0, 1fr)');
        const visitor = styles('.st-inline-visitors .st-visitor', width, height);
        if (width <= 600) {
            assert.equal(visitor.display, 'flex');
            assert.equal(visitor['flex-direction'], 'column');
            assert.equal(styles('.st-content-info', width, height).display, 'contents');
            assert.equal(styles('.st-content-metrics', width, height)['grid-template-columns'], 'repeat(2, minmax(0, 1fr))');
        } else {
            assert.equal(visitor.display, 'grid');
            assert.match(visitor['grid-template-columns'], /minmax\(160px,/);
        }
        assert.equal(styles('.st-metrics', width, height)['grid-template-columns'], 'repeat(2, minmax(0, 1fr))');
        assert.equal(styles('.st-duration-pair', width, height)['grid-template-columns'], 'repeat(2, minmax(0, 1fr))');
        assert.equal(styles('.st-inline-trail', width, height)['grid-column'], '1/-1');
    });
    test(`${width}×${height}: dialogs scroll within dynamic viewport and safe areas`, () => {
        const dialog = styles('.st-dialog', width, height), overlay = styles('.st-overlay', width, height);
        assert.equal(dialog['min-height'], '0');
        assert.equal(dialog.overflow, 'auto');
        assert.equal(dialog['overscroll-behavior'], 'contain');
        assert.match(dialog['max-height'], /100dvh/);
        assert.match(dialog['max-height'], /safe-area-inset-top/);
        assert.match(dialog['max-height'], /safe-area-inset-bottom/);
        assert.match(overlay.padding, /safe-area-inset-left/);
        assert.equal(styles('.st-dialog > header > button', width, height)['flex-shrink'], '0');
        assert.equal(styles('.st-dialog > header', width, height).position, height <= 500 ? 'static' : 'sticky');
        assert.equal(styles('.st-dialog .st-media img', width, height)['max-height'], height <= 500 ? '50dvh' : 'min(560px, 60dvh)');
    });
}

test('focus, wrapping, error and motion states are visible rather than clipped', () => {
    assert.equal(styles('button:focus-visible').outline, '3px solid var(--st-focus)');
    assert.equal(styles('a:focus-visible')['outline-offset'], '3px');
    assert.equal(styles('strong')['overflow-wrap'], 'anywhere');
    assert.equal(styles('.st-content-title')['-webkit-line-clamp'], undefined);
    assert.equal(styles('.st-media-failed').height, 'auto');
    assert.equal(styles('.st-feedback')['background'], 'var(--st-surface)');
    assert.equal(styles('*', 390, 844, true).animation, 'none');
    assert.equal(styles('*', 390, 844, true).transition, 'none');
});
