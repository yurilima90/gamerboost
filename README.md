# GamerBoost

Otimizador gratuito para PC gamer (feito para Ryzen 5 1600 + RX 580, Windows 10/11).
77 ajustes com descrição, scanner de hardware, limpeza, ponto de restauração,
monitor ao vivo e benchmark com nota para comparar antes e depois.

## Usar
1. Abra a aba **Releases** deste repositório e baixe `GamerBoost.exe`.
2. Dê dois cliques e aceite o aviso de administrador.
3. O Windows pode mostrar "O Windows protegeu seu PC" (o programa não tem assinatura paga):
   clique em **Mais informações → Executar assim mesmo**.
4. Depois da primeira instalação, o próprio programa avisa quando há versão nova
   (botão **Atualizar agora**; o arquivo é conferido por SHA-256).

## Compilar no seu PC
Instale o [Go](https://go.dev/dl) e execute `build.bat`.

## Publicar uma versão nova
Crie uma tag, por exemplo `v3.3.0`. O GitHub Actions compila o EXE e publica em Releases:

    git tag v3.3.0
    git push origin v3.3.0

## Segurança
- Cada ajuste guarda o valor original e pode ser revertido.
- O atualizador só baixa arquivos deste repositório e recusa qualquer arquivo cujo SHA-256 não confira.
