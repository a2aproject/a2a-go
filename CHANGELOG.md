# Changelog

## 0.3.0 (2025-11-04)


### Features

* add JSON-RPC client transport implementation ([#79](https://github.com/a2aproject/a2a-go/issues/79)) ([1690088](https://github.com/a2aproject/a2a-go/commit/16900888cf3e84822b84611a8b85aefab7d1044f))
* agent card resolver ([#48](https://github.com/a2aproject/a2a-go/issues/48)) ([0951293](https://github.com/a2aproject/a2a-go/commit/0951293e320a35202d2ca51a1761adb6e769419a))
* blocking flag handling ([#97](https://github.com/a2aproject/a2a-go/issues/97)) ([f7aa465](https://github.com/a2aproject/a2a-go/commit/f7aa4653452b4f845facf237802ee9ab0e52846f)), closes [#96](https://github.com/a2aproject/a2a-go/issues/96)
* client API proposal ([#32](https://github.com/a2aproject/a2a-go/issues/32)) ([b6ca54f](https://github.com/a2aproject/a2a-go/commit/b6ca54fa76f3a6d9c90e89d0dd7569442a1e9149))
* client auth interceptor ([#90](https://github.com/a2aproject/a2a-go/issues/90)) ([25b9aae](https://github.com/a2aproject/a2a-go/commit/25b9aae1cdb6dece5c2b6cdb32716e4d2ebbd021))
* client interceptor invocations ([#51](https://github.com/a2aproject/a2a-go/issues/51)) ([3e9f2ae](https://github.com/a2aproject/a2a-go/commit/3e9f2aef25c67a0cef56823b5f282a11cea59bb6))
* core types JSON codec ([#42](https://github.com/a2aproject/a2a-go/issues/42)) ([c5b3982](https://github.com/a2aproject/a2a-go/commit/c5b3982a41aa01c428ad0e3b56aadc99157b23ee))
* define core types and interfaces ([#16](https://github.com/a2aproject/a2a-go/issues/16)) ([69b96ea](https://github.com/a2aproject/a2a-go/commit/69b96ea0715cbdefe6d22f08e3fb0a11755f9476))
* disallow custom types and circular refs in Metadata ([#43](https://github.com/a2aproject/a2a-go/issues/43)) ([53bc928](https://github.com/a2aproject/a2a-go/commit/53bc9283dddd591a3563e6b1ea070b1972967bfa))
* get task implementation ([#59](https://github.com/a2aproject/a2a-go/issues/59)) ([f74d854](https://github.com/a2aproject/a2a-go/commit/f74d85423c678a907ae3a0f95cdb94ae3f2ebe1e))
* grpc authenticated agent card and producer utils ([#85](https://github.com/a2aproject/a2a-go/issues/85)) ([9d82f31](https://github.com/a2aproject/a2a-go/commit/9d82f31874995065d2dac2afbb5c408ab9a42fc8)), closes [#82](https://github.com/a2aproject/a2a-go/issues/82)
* grpc client transport ([#66](https://github.com/a2aproject/a2a-go/issues/66)) ([fee703e](https://github.com/a2aproject/a2a-go/commit/fee703e5d87e1c48fffe8138d8b57c1f37556bb8))
* grpc code generation from A2A .proto spec ([#11](https://github.com/a2aproject/a2a-go/issues/11)) ([2993b98](https://github.com/a2aproject/a2a-go/commit/2993b9830c072cfc6bc1feac81ad6695fc919a3a))
* handling artifacts and implementing send message stream ([#52](https://github.com/a2aproject/a2a-go/issues/52)) ([c3fa631](https://github.com/a2aproject/a2a-go/commit/c3fa6310a7b67d7f0771e688bbbd00730950ddb6))
* implement an a2aclient.Factory ([#50](https://github.com/a2aproject/a2a-go/issues/50)) ([49deee7](https://github.com/a2aproject/a2a-go/commit/49deee794474104bb7ebaf281895e6dd47d03f0c))
* implementing grpc server wrapper ([#37](https://github.com/a2aproject/a2a-go/issues/37)) ([071e952](https://github.com/a2aproject/a2a-go/commit/071e9522534e7aeaf0375451a73dc0b175e516b4))
* implementing message-message interaction ([#34](https://github.com/a2aproject/a2a-go/issues/34)) ([b568979](https://github.com/a2aproject/a2a-go/commit/b5689797dc63c25c2e8165830dc5f556ce784ad3))
* implementing task pushes ([#86](https://github.com/a2aproject/a2a-go/issues/86)) ([c210240](https://github.com/a2aproject/a2a-go/commit/c210240cc39787c6a66b508d4a5cb976612d5c5a))
* input-required and auth-required handling ([#70](https://github.com/a2aproject/a2a-go/issues/70)) ([3ac89ba](https://github.com/a2aproject/a2a-go/commit/3ac89ba98318964a960be7ae6b2be07909e7ac75))
* jsonrpc server ([#91](https://github.com/a2aproject/a2a-go/issues/91)) ([5491030](https://github.com/a2aproject/a2a-go/commit/549103074cdcf8f3a12cdd1f0bcbbc3a599dd0f1))
* logger ([#56](https://github.com/a2aproject/a2a-go/issues/56)) ([86ab9d2](https://github.com/a2aproject/a2a-go/commit/86ab9d2e8e41b27fd605c4025f04ffe1fcdcd368))
* request context loading ([#60](https://github.com/a2aproject/a2a-go/issues/60)) ([ab7a29b](https://github.com/a2aproject/a2a-go/commit/ab7a29b1ff309361fcb240f9fb0d4eb00c022c53))
* result aggregation part 1 - task store ([#38](https://github.com/a2aproject/a2a-go/issues/38)) ([d3c02f5](https://github.com/a2aproject/a2a-go/commit/d3c02f578ce72ce0ba2bf15299afc07d88f75594))
* result aggregation part 3 - concurrent task executor ([#40](https://github.com/a2aproject/a2a-go/issues/40)) ([265c3e7](https://github.com/a2aproject/a2a-go/commit/265c3e7f183aa79cbbd1d3cba02cdb24d43d80f5))
* result aggregation part 4 - integration ([#41](https://github.com/a2aproject/a2a-go/issues/41)) ([bab72d9](https://github.com/a2aproject/a2a-go/commit/bab72d9c72aa13614b2fac74925eb158c1daf91f))
* SDK type utilities ([#31](https://github.com/a2aproject/a2a-go/issues/31)) ([32b77b4](https://github.com/a2aproject/a2a-go/commit/32b77b492b838f0f6284ce63ed0558886c811781))
* server middleware API ([#63](https://github.com/a2aproject/a2a-go/issues/63)) ([738bf85](https://github.com/a2aproject/a2a-go/commit/738bf85565ebe190e163c87fb79d695254a4438b))
* server middleware integration ([#64](https://github.com/a2aproject/a2a-go/issues/64)) ([5dc8be0](https://github.com/a2aproject/a2a-go/commit/5dc8be03b20f96a684ce3703c14d6c6b5f9234ee))
* smarter a2aclient ([#88](https://github.com/a2aproject/a2a-go/issues/88)) ([322d05b](https://github.com/a2aproject/a2a-go/commit/322d05bc4fb73b4316adf88bbf3c42fb1e73379a))
* task event factory ([#95](https://github.com/a2aproject/a2a-go/issues/95)) ([fbf3bcf](https://github.com/a2aproject/a2a-go/commit/fbf3bcff0c4af7424733aeaf02a7b982c9b6e743)), closes [#84](https://github.com/a2aproject/a2a-go/issues/84)
* task executor docs ([#36](https://github.com/a2aproject/a2a-go/issues/36)) ([b6868df](https://github.com/a2aproject/a2a-go/commit/b6868df38d11f097e7a8d71bfec2d91ec9e7399e))
* task update logic ([0ac987f](https://github.com/a2aproject/a2a-go/commit/0ac987fcacd94d374ea9141ca917afa12814665f))


### Bug Fixes

* Execute() callers missing events ([#74](https://github.com/a2aproject/a2a-go/issues/74)) ([4c3389f](https://github.com/a2aproject/a2a-go/commit/4c3389f887cbcc0d402a5d20a7a7112d5890f64d))
* mark task failed when execution fails ([#94](https://github.com/a2aproject/a2a-go/issues/94)) ([ee0e7ed](https://github.com/a2aproject/a2a-go/commit/ee0e7ed693792782f4c3e0b3dc2361f03007a91f))
* push semantics update ([#93](https://github.com/a2aproject/a2a-go/issues/93)) ([76bff9f](https://github.com/a2aproject/a2a-go/commit/76bff9f8fadd4d2d9611c872d07d4efc626bafaf))
* race detector queue closed access ([c07b7d0](https://github.com/a2aproject/a2a-go/commit/c07b7d0014056a6b499d0363a13a3efc7b03519b))
* regenerate proto and update converters ([#81](https://github.com/a2aproject/a2a-go/issues/81)) ([c732060](https://github.com/a2aproject/a2a-go/commit/c732060cb007a661a059fe51b9a3907fb1790af5))
* streaming ([#92](https://github.com/a2aproject/a2a-go/issues/92)) ([ca7a64b](https://github.com/a2aproject/a2a-go/commit/ca7a64bd7b6d056b40b56b6ba14b38ef2eea8f8a))


### Miscellaneous Chores

* release 0.3.0 ([fa7cfba](https://github.com/a2aproject/a2a-go/commit/fa7cfbad3bbcf9fefe91a7fb769ee34f074a700a))
