import * as React from 'react';

export type FeatherIcon = (props: React.SVGProps<SVGSVGElement> & {
    children?: React.ReactNode;
    size?: string | number;
    color?: string;
    title?: string;
}) => React.ReactElement | null;

export {};
