declare module 'miniprogram-sm-crypto' {
  export interface SM4Options {
    mode?: 'ecb' | 'cbc';
    iv?: string;
    padding?: 'pkcs#5' | 'pkcs#7' | 'none';
    output?: 'string' | 'array';
  }

  export interface SM4 {
    encrypt(msg: string | number[], key: string | number[], options?: SM4Options): string | number[];
    decrypt(encryptData: string | number[], key: string | number[], options?: SM4Options): string | number[];
  }

  export interface SM2 {
    generateKeyPairHex(entropy?: string | number, random?: any): {
      publicKey: string;
      privateKey: string;
    };
    doEncrypt(msg: string | number[], publicKey: string, cipherMode?: number): string | number[];
    doDecrypt(encryptData: string | number[], privateKey: string, cipherMode?: number, options?: any): string | number[];
    doSignature(msg: string | number[], privateKey: string, options?: any): string;
    doVerifySignature(msg: string | number[], sigValueHex: string, publicKey: string, options?: any): boolean;
  }

  const smCrypto: {
    sm2: SM2;
    sm4: SM4;
  };

  export default smCrypto;
}
