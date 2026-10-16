import { useEffect, useState } from 'react';

/** 是否移动端视口（与 layout.tsx / 各页 768px 断点保持一致） */
export function useIsMobile(query = '(max-width: 768px)'): boolean {
    const [matches, setMatches] = useState(() => window.matchMedia(query).matches);
    useEffect(() => {
        const mq = window.matchMedia(query);
        const onChange = (event: MediaQueryListEvent) => setMatches(event.matches);
        mq.addEventListener('change', onChange);
        return () => mq.removeEventListener('change', onChange);
    }, [query]);
    return matches;
}
