declare module 'crypto-js' {
  export interface WordArray {
    words: number[];
    sigBytes: number;
    toString(encoder?: any): string;
  }

  export namespace enc {
    class Base64 {
      static stringify(wordArray: WordArray): string;
      static parse(base64Str: string): WordArray;
    }
    class Hex {
      static stringify(wordArray: WordArray): string;
      static parse(hexStr: string): WordArray;
    }
    class Utf8 {
      static stringify(wordArray: WordArray): string;
      static parse(utf8Str: string): WordArray;
    }
  }

  export function HmacSHA256(message: string, key: string): WordArray;
  export function MD5(message: string): WordArray;
  export function SHA256(message: string): WordArray;
}
