-- Nova origem de entrada no Banco de Talentos: a pessoa foi APROVADA numa vaga (chegou à fase final,
-- Selecionados) e já tinha consentido com o uso dos dados para futuras oportunidades. O perfil de quem
-- deu certo é a referência de comparação mais valiosa do banco — é o "como é um contratado".
--
-- Arquivo separado de 0012 de propósito: valor novo de enum não pode ser usado na mesma transação que
-- o declara, e 0012 usa 'aprovacao'.
alter type talent_origin add value if not exists 'aprovacao';
