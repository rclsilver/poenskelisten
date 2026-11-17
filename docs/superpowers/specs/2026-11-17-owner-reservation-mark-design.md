# Marque « réservé » posée par le propriétaire et les contributeurs

Date : 2026-11-17
Branche de base : `origin/contrib` (`de0676a`, contrib reconstruite sur la v2.7.0).
Statut : en attente d'approbation.

## Intention et pourquoi

Cas d'usage réel : un couple tient sur Pønskelisten la liste d'envies de **leur
fille**. Ce ne sont pas les destinataires de la liste : ils en sont les
**coordinateurs**. Au fil des conversations, des proches leur disent « je prends
tel cadeau ». Les parents veulent alors **marquer le souhait comme réservé** pour
qu'un autre proche qui consulte la liste (via le **lien public**) voie que c'est
déjà pris et évite le doublon.

Le problème : le mécanisme de réservation existant (`WishClaim`) est bâti sur un
principe de **surprise**. Le propriétaire et les contributeurs y sont à la fois
*empêchés de réserver* et *rendus aveugles* aux réservations (`controllers/wish.go`
lignes 170-183 et 730-737), et la vue publique efface toute réservation
(`controllers/wishlist.go:1489-1495`). Ce principe protège le destinataire quand
il possède lui-même sa liste ; il n'a pas de sens ici, où le destinataire (la
fille) ne possède pas la liste.

La solution retenue n'est donc **pas** de modifier la réservation-surprise, mais
d'ajouter **à côté** un second concept, distinct : une marque « réservé » posée
par le côté propriétaire, qu'il contrôle et qu'il voit.

## Objectifs

- Le **propriétaire** d'une liste et ses **contributeurs** peuvent marquer un
  souhait comme « réservé », et retirer cette marque.
- Ils peuvent attacher à la marque une **note facultative** (« c'est tante Claire
  qui l'apporte ») — **interne**, visible uniquement du propriétaire et des
  contributeurs.
- Sur le **lien public**, un souhait marqué apparaît comme « réservé » (état seul,
  **sans** la note).
- Fonctionne sur **n'importe quelle liste** (privée, partagée à un groupe,
  publique), indépendamment du drapeau « réservable ».

## Non-objectifs

- Ne pas toucher à la réservation-surprise (`WishClaim`) : ni sa logique
  d'autorisation, ni le secret imposé au propriétaire/contributeurs, ni l'index
  « une réservation vivante par souhait », ni la purge de la vue publique.
- Pas d'outil MCP pour cette marque dans cette première passe (suite possible).
- Pas d'attribution nominative du proche côté données structurées : le « qui
  apporte » vit librement dans la note interne, pas dans un champ utilisateur.
- Pas de notification e-mail liée à cette marque.

## Anti-objectifs (ce qui signerait un échec, même si « ça marche »)

- Qu'un proche sur le lien public puisse **lire la note interne**.
- Qu'un destinataire tenant **sa propre** liste d'envies voie soudain qui lui a
  réservé quoi (régression de la surprise). La nouvelle marque ne doit jamais
  révéler un `WishClaim`, ni l'inverse : le propriétaire/contributeur reste aveugle
  aux réservations-surprise et ne voit le badge « Réservé » que pour **ses propres
  marques**.

## Contraintes

- Base de données via GORM, migrations par `AutoMigrate` dans
  `database/client.go` (SQLite/PostgreSQL/MySQL). Pas de nouvelle variable
  d'environnement attendue.
- Autorisation « propriétaire ou contributeur » déjà disponible et réutilisée
  telle quelle : `database.VerifyUserOwnershipToWishlist(userID, wishlistID)` +
  `database.VerifyWishlistCollaboratorToWishlist(wishlistID, userID)` (même motif
  que `RegisterWish`, `controllers/wish.go:306-325`). Ces helpers prennent un
  `wishlist_id` ; les endpoints sont routés sur `:wish_id`, donc le contrôleur
  **charge d'abord le souhait** pour obtenir son `WishlistID` (souhait absent =
  même refus silencieux que « non autorisé », pas de fuite d'existence).
- La vue publique (`GetPublicWishlist`) part d'un objet complet puis **retire**
  les champs sensibles : il faut donc décider explicitement de **laisser passer**
  l'état réservé et de **retirer** la note.

## Approches envisagées

- **A — drapeau booléen sur `Wish`.** Minimal, mais binaire : aucune note, aucune
  trace de qui a posé la marque. Écartée car l'utilisateur veut pouvoir noter qui
  apporte le cadeau.
- **B — table séparée `WishReservation` (retenue).** Indépendante de `WishClaim`,
  porte l'état, la note interne, l'auteur de la marque et les horodatages. Un peu
  plus de code, mais c'est le besoin exprimé et ça garde la réservation-surprise
  totalement intacte.
- **C — réutiliser `WishClaim` avec un indicateur « manuelle ».** Écartée :
  entrelace la marque avec exactement la logique de secret qu'on veut laisser
  intacte (purge, index unique, strip public) — risque élevé de fuite ou de
  régression.

Choix de l'utilisateur : **B**.

## Conception décidée

### Donnée

Nouveau modèle `models.WishReservation` :

- `GormModel` (ID, horodatages, soft-delete).
- `WishID uuid.UUID` — le souhait concerné.
- `ReservedByUserID uuid.UUID` — qui (propriétaire ou contributeur) a posé/modifié
  la marque en dernier. Interne.
- `Note string` — note libre facultative. Interne.
- `Enabled bool` (défaut true) — pour un retrait en soft-delete cohérent avec le
  reste du schéma.

Règle : **au plus une réservation vivante par souhait** (`enabled` + non supprimée).
Posée à neuf si absente, sinon mise à jour (note / auteur). Le retrait désactive la
ligne.

**Unicité à garantir sur les trois moteurs**, en répliquant exactement le durcissement
déjà fait pour les réservations-surprise (la couche applicative seule ne suffit pas
sur MySQL, qui n'a pas d'index partiel) :
- SQLite / PostgreSQL : index unique partiel `WHERE enabled AND deleted_at IS NULL`,
  créé au démarrage à la manière de `database.EnsureOneClaimPerWish`
  (`database/wish.go:286-301`).
- MySQL : transaction avec `SELECT ... FOR UPDATE` sur le souhait avant insertion,
  à la manière de `CreateWishClaimInDB` (`database/wish.go:244-268`).
C'est un coût de code assumé, pour la cohérence avec l'existant.

Migration : ajouter `Instance.AutoMigrate(&models.WishReservation{})` dans
`database/client.go`, à la suite des autres (après `WishClaim`).

### API (routes authentifiées, `main.go` vers la ligne 299)

- `POST   /api/auth/wishes/:wish_id/reservation` — crée ou met à jour la marque
  (corps : `{ "note": "..." }`, note facultative).
- `DELETE /api/auth/wishes/:wish_id/reservation` — retire la marque.

Autorisation des deux : **propriétaire OU contributeur** de la liste du souhait,
via les deux helpers cités. Tout autre utilisateur (y compris un membre de groupe)
reçoit le même refus. UUID nul / souhait inexistant : refus identique, sans fuite
d'existence (cohérent avec le reste du fork).

Nouveau contrôleur, p. ex. `controllers/wishreservation.go` : `SetWishReservation`
et `RemoveWishReservation`.

### Exposition dans les objets

`models.WishObject` gagne un champ dédié, p. ex. :

```
Reservation *WishReservationObject `json:"reservation"`
```

avec `WishReservationObject { Reserved bool; Note string; ... }`. `Reserved` vaut
vrai ssi une ligne vivante (`enabled`, non supprimée) existe. `ReservedByUserID`
n'est **jamais** sérialisé dans l'objet (interne). Rempli dans
`ConvertWishToWishObject` (`controllers/wish.go`, même endroit que `WishClaim`).

Visibilité de la **note** :

- Propriétaire / contributeurs : voient `Reserved` **et** `Note`.
- Membres de groupe (listes partagées) : voient `Reserved`, **pas** la note.
- Vue publique : voit `Reserved`, **pas** la note (voir ci-dessous).

`Reserved` est visible de tous ceux qui voient le souhait ; seule la note est
restreinte au côté propriétaire. La restriction se décide sur le **propriétaire de
la liste + ses contributeurs** (via `wishlistOwnerUser.ID` et `wishlistCollabObjects`,
déjà calculés dans `ConvertWishToWishObject` — **pas** sur `wish.OwnerID`). Même
esprit que la purge existante mais sens inverse : on remplit l'objet complet puis on
**vide `Note`** pour qui n'est ni propriétaire ni contributeur.

### Vue publique (`GetPublicWishlist`, `controllers/wishlist.go:1489-1495`)

`GetPublicWishlist` appelle `ConvertWishlistToWishlistObject(..., nil)` : avec
`requestUserID == nil`, la note est **déjà** vidée par `ConvertWishToWishObject`
(cf. visibilité ci-dessus). La boucle de nettoyage n'a donc qu'à **laisser
`Reservation.Reserved` intact** — surtout ne pas l'effacer en même temps que la
purge `wish.WishClaim = []` de la ligne 1494. La purge des `WishClaim` reste telle
quelle.

### Badge « Réservé » : un seul mot, un seul badge

On réutilise le vocabulaire existant : **« Réservé »** (clé `wish.claimed` déjà
présente, `web/json/lang-fr.json:418`). Pour celui qui regarde, « réservé par un
proche » et « réservé par le coordinateur » veulent dire la même chose — *c'est
pris*. On n'introduit **aucun mot nouveau** pour la marque du propriétaire.

Un souhait n'affiche qu'**un seul** badge « Réservé », vrai dès qu'il est pris. Ce
n'est **pas le vocabulaire** mais la **visibilité** qui respecte le secret :

- **Propriétaire / contributeurs** : badge « Réservé » **uniquement pour leurs
  propres marques** (`reservation.reserved`). Ils restent aveugles aux
  réservations-surprise, inchangé.
- **Membres d'un groupe** et **lien public** : badge « Réservé » dès que le souhait
  est pris, que ce soit par une réservation-surprise **ou** par une marque du
  coordinateur. Sur une liste réservable partagée, le badge est donc un **OU** des
  deux, affiché une seule fois (jamais deux badges).

Dans le scénario de l'utilisateur (liste publique, donc non réservable), il n'y a
aucune réservation-surprise : le badge ne reflète que la marque du coordinateur.

### Interface web

- **Cartes de souhait** (`web/js/cards.js`, partagé par la vue connectée et la vue
  publique) : réutiliser le `statusBadge` existant ; le rendre vrai aussi quand
  `reservation.reserved` l'est, selon la règle de visibilité ci-dessus. La note
  n'est jamais envoyée au public.
- **Contrôle propriétaire/contributeur** (vue connectée, `cards.js` /
  `wishlist.js`) : une bascule « Marquer comme réservé / Retirer » et un champ de
  note, visibles seulement quand l'utilisateur est propriétaire ou contributeur de
  la liste. Appellent les deux nouveaux endpoints.
- i18n : réutiliser `wish.claimed` pour le badge ; nouvelles clés FR/EN seulement
  pour la bascule et le libellé de note.

Esquisse (carte connectée, côté propriétaire) :

```
┌───────────────────────────────┐
│ [img]  Poupée Corolle         │
│        45 €        [Réservé ✓] │
│        Note : tante Claire 🖊  │
│        [ Retirer la réserv. ]  │
└───────────────────────────────┘
```

Esquisse (même souhait, lien public, proche) :

```
┌───────────────────────────────┐
│ [img]  Poupée Corolle         │
│        45 €        [Réservé]   │   ← pas de note, pas de bascule
└───────────────────────────────┘
```

## Interaction avec la réservation-surprise

Les deux concepts restent **indépendants en données** : la marque du coordinateur
ne lit ni ne modifie un `WishClaim`, et inversement. On n'impose **aucun verrou dur**
entre les deux (un tel verrou se heurterait à l'aveuglement du propriétaire sur les
listes classiques, et n'apporte rien au cas d'usage publique). Côté affichage, ils
convergent en un **seul badge « Réservé »** selon la règle de visibilité ci-dessus.

Sur une liste publique (non réservable), aucune réservation-surprise n'existe : seul
le nouveau concept s'applique — c'est le cas de l'utilisateur.

## Laissé au constructeur

- Noms exacts des symboles (`WishReservation`, champs, contrôleurs, clés i18n) et
  emplacement précis des fichiers, en suivant les conventions du dépôt.
- Mise en œuvre fine de l'unicité « une seule vivante » (l'approche est décidée
  ci-dessus : index partiel SQLite/PostgreSQL + transaction `FOR UPDATE` MySQL).
- Rendu visuel fin du badge et de la bascule, dans le style de l'UI Atelier
  existante.
- Tests : couche DB (création/mise à jour/retrait, une seule vivante), endpoints
  (autorisation propriétaire/contributeur, refus des autres, note cachée hors
  propriétaire), vue publique (état visible, note vide), non-régression de
  `WishClaim` (le secret et la purge inchangés). Chaque nouveau test doit échouer
  sur le commit précédent.

## Vérification de fin (rappel pour l'implémentation)

`gofmt` vide, `go build`, `go vet`, `go test -race ./...` vert, tests web verts,
en suivant le pipeline habituel du fork. Aucun secret. Rien poussé ni déployé sans
accord explicite.
