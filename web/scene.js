// Ảnh nhà và vùng từng phòng trên ảnh, đơn vị % theo chiều rộng / cao của ảnh: [x, y, rộng, cao].
// Khoá phòng = id khu trong config.yaml. Căn vùng: mở /?demo&vung để thấy khung từng phòng rồi sửa số ở đây.
const SCENE = {
  house: {
    src: '/img/nha.webp', w: 1024, h: 687,
    floors: {3: [8, 33], 2: [33, 58], 1: [58, 94]},   // dải dọc [từ, tới] của từng tầng
    rooms: {
      gieng_troi: [38.6, 14.6, 29.8, 17.8], kythuat: [68.4, 13.8, 10.9, 18.5],
      lamviec: [22.5, 33.8, 18.6, 23.3], ngu1: [41.5, 33.8, 15.6, 23.3], ngu2: [57.6, 33.8, 10.7, 23.3],
      docsach: [78.6, 33.8, 10.3, 23.3],
      santruoc: [7.3, 68.4, 21, 24.7], cafe: [29.3, 58.2, 26.4, 34.2], bep: [55.7, 58.2, 12.7, 34.2],
      kho: [68.4, 58.2, 9.8, 34.2], wc: [78.6, 58.2, 6.8, 34.2], sansau: [85.9, 58.2, 8.3, 30],
    },
  },
  floors: {
    1: {src: '/img/tang1.webp', w: 2000, h: 667, wifi: [55.3, 33], rooms: {
      santruoc: [0, 25, 18.5, 56], cafe: [20, 26, 33.75, 51.7], bep: [56.5, 26, 14, 51.7],
      kho: [70.75, 26, 10.5, 51.7], wc: [82, 26, 6.75, 51.7], sansau: [89.5, 26, 10.5, 51.7]}},
    2: {src: '/img/tang2.webp', w: 2000, h: 667, wifi: [76.5, 46], rooms: {
      lamviec: [5.25, 28.5, 25.5, 48.7], ngu1: [31.5, 28.5, 18.5, 48.7], ngu2: [50.75, 28.5, 17.75, 48.7],
      docsach: [80, 28.5, 18, 48.7]}},
    3: {src: '/img/tang3.webp', w: 2000, h: 667, wifi: [79, 40], rooms: {
      gieng_troi: [3.75, 28.5, 50.75, 59], kythuat: [56.25, 27.7, 41.25, 59.3]}},
  },
  // Ảnh thật từng phòng (16:9). Phòng chưa có ảnh: phóng to vùng phòng đó trên ảnh tầng.
  photos: {santruoc: '/img/phong-santruoc.webp', cafe: '/img/phong-cafe.webp', bep: '/img/phong-bep.webp'},
};
