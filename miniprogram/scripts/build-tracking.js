'use strict';
// New SDK sources intentionally use the JS-compatible TypeScript subset.
// This deterministic build avoids relying on a developer's IDE transpilation settings.
const fs=require('node:fs'),path=require('node:path');
const root=path.join(__dirname,'../miniprogram');
const sources=fs.readdirSync(path.join(root,'utils/tracking')).filter(x=>x.endsWith('.ts')).map(x=>'utils/tracking/'+x).concat([
 'utils/profile-access.ts',
 'utils/protected-media.ts',
 'utils/profile-request-guard.ts',
 'utils/phase3-api.ts',
 'utils/project-collections-api.ts',
 'utils/project-packages-api.ts',
 'pages/projectcollections/projectcollections.ts',
 'pages/collectionreader/collectionreader.ts',
 'pages/projectpackage/projectpackage.ts',
 'pages/projectpackageview/projectpackageview.ts',
 'pages/projectinvite/projectinvite.ts',
 'pages/contentreader/contentreader.ts',
 'pages/messagecenter/messagecenter.ts',
 'pages/contentworkbench/contentworkbench.ts',
 'pages/contentedit/contentedit.ts',
 'pages/contentwriting/contentwriting.ts',
 'pages/nativereferences/nativereferences.ts',
 'pages/sharevisits/sharevisits.ts',
 'pages/visitdetail/visitdetail.ts'
]);
let failed=false;
for(const source of sources){const file=path.join(root,source),js=file.replace(/\.ts$/,'.js');const output=fs.readFileSync(file,'utf8').replace(/^\/\/ @ts-nocheck\n/, '').replace(/\nexport \{\};\n?$/, '\n');if(process.argv.includes('--check')){if(fs.readFileSync(js,'utf8')!==output){console.error('Source/build mismatch: '+source);failed=true;}}else fs.writeFileSync(js,output);}
if(failed)process.exitCode=1;
